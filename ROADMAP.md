# Ripper Roadmap

Converting `scripts/media-ripper.sh` from a bash-orchestrated pipeline into Go,
in two stages:

1. **This pass — extract logic.** Move all computation / data-shaping out of bash
   into native Go functions. Originally bash was to keep the two streaming commands
   (`makemkvcon`, `rsync`); in practice the Go rip path went further — it drives
   `makemkvcon` directly and replaced `rsync` with a verified in-Go copy. One-shot
   external tools (`clamdscan`, `eject`, `udevadm`, `smart_check`) stay in bash for
   now. The ported path is written but not yet wired into the CLI (see Integration).
2. **Next — daemon.** A long-lived server owns state and runs a goroutine per rip.
   The CLI becomes a thin client that talks to the server over a unix socket. The
   server drives `makemkvcon`/`rsync` via `exec`, holds status in memory, and
   pushes to the web UI over SSE. Bash shrinks to nothing (or near it).

## Target architecture (stage 2)

- One daemon process; `map[driveTag]*Rip` of live jobs.
- CLI `ripper rip 0 2` → `POST /rips` over unix socket → server validates
  synchronously (drive busy? bad season?) and starts a goroutine.
- In-memory status = single source of truth → SSE to the browser (replaces the
  `<meta http-equiv="refresh">` full-page reload).
- Contention, locking, cancellation collapse to in-process primitives
  (map iteration, `sync.Mutex`, `context.Context`).
- Server is the single sqlite writer (instead of every CLI invocation opening the DB).

### Tradeoffs accepted for the daemon (decide deliberately)

- **Restart kills in-flight rips.** Detached processes survive a `serve` restart
  today; the daemon does not. Need a story: refuse-restart-while-ripping / graceful
  drain / systemd `ExecStop` wait.
- **Crash blast radius.** One process → a panic/OOM can take down all drives.
  Mitigate with `recover()` per goroutine, keep `makemkvcon` a subprocess, and
  keep a DB checkpoint for recovery (see Crash recovery below).

### Crash recovery — Level 1: checkpoint + resume

Chosen approach: **everything dies together; resume from a durable checkpoint on
startup.** No survive-in-place, no reparenting — makemkvcon stays an ordinary child
subprocess of the daemon.

Why not "keep makemkvcon alive across a daemon restart" (the survive-in-place idea):

- You can only `wait()` on your own child. After a restart the new daemon isn't the
  parent, so it can never get makemkvcon's exit *status* — only liveness
  (`kill(pid,0)` / `/proc` / `pidfd_open`). Outcome would have to be inferred from
  output files anyway.
- systemd's default `KillMode=control-group` SIGTERMs every process in the cgroup on
  restart, so a child doesn't survive for free — it'd need `KillMode=mixed` plus its
  own process group (`SysProcAttr{Setpgid:true}`). Not worth it for Level 1.

Recovery granularity: makemkvcon **cannot** resume a half-ripped title, so the atom
is a *completed track*. Because each track rips to its own subdir (`$BATCH_DIR/t$id`),
completion is verifiable straight from the filesystem — the subdir holds one
plausibly-sized mkv — even when no exit code survived.

Durable job checkpoint (sqlite; daemon is the single writer; distinct from the
ephemeral display status):

```
job: drive_tag, disc_raw_title, batch_dir, dest_rel, season
     selected_ids:  [0,3,4,5]
     completed_ids: [0,3]        # verified: subdir has a full mkv
```

On startup, for each unfinished job:

1. Is the **same** disc still in the drive? Match `disc_raw_title` — never resume
   disc A's track ids onto a disc B someone swapped in. If absent/different → mark failed.
2. Discard the in-progress title's partial file.
3. Re-rip the remaining ids (`selected_ids − completed_ids`), then continue the
   normal sort/scan/promote pipeline.

Worst case for a mid-rip restart: re-rip **one title** (~5–15 min), never the whole disc.

Level 2 (survive-in-place: makemkvcon keeps ripping through a daemon restart, daemon
re-attaches via pidfd + `KillMode=mixed`) is deferred — a later optimization only if
re-ripping one title on restart proves annoying. Nothing in Level 1 blocks it.

---

## Stage 1 checklist — extract logic to Go

Rule of thumb: computation & data-shaping → native Go func. Only tools with no
Go-native equivalent stay as external process calls.

### Bucket A — pure logic → native Go (no external process)

- [x] `ParseInfo` — track parsing → `internal/makemkv/parse.go`.
- [x] title cleaning — folded into `ParseInfo` (strip SEASON/DISC, titlecase).
- [x] dest-path derivation — `Disc.setDests` (Movie vs `Show/Season N`).
- [x] track selection — anchor-band / largest in `Disc.Rip`; an explicit
      `SelTracks` list overrides (the data-level `-t` hook).
- [x] episode naming / rename — folded into `Disc.Rip` (offset scan +
      per-track dirs). The old `PlanRename` / source-order mapping is deleted,
      not ported — the per-track dirs made it moot.
- [ ] `contention` over `[]Status` — `reserved_by_others` + `sibling_in_starting`
      (174–216).
- [~] `capacity` — **partially done, reshaped from the bash design.** Ported as a
      per-track guard in `Disc.Rip` (not a whole-disc pre-check): before each track,
      `sysstat.AvailBytes` vs `track.Bytes * 11/10` (10% headroom), fail the track if
      short. Rationale: `promote` drains staging per track, so peak staging use is
      ~one track, never the whole disc — the bash `NEEDED = TOTAL_MB * 11/10`
      whole-disc reservation was over-conservative. The "need" now comes from parsed
      `Track.Bytes` (exact), not `du`/`TOTAL_MB` (which truncated per-title).
      **Deliberately dropped, not deferred:** the `reserved_by_others` reservation
      math — staging (~100 GB) vs max real track (~40 GB) means two concurrent rips
      fit, so the cross-drive TOCTOU race is harmless; the daemon's in-memory state
      closes it properly later anyway. **Skipped for now (may revisit):** the
      `.review` eviction wait (578–603) — trialing whether staging drains on its own
      given the tighter per-track footprint; without it, a space-short track just
      fails instead of evicting.
- [ ] index resolution — parse `DRV:` lines → device↔disc index (381–405).
      **Decide first:** the `dev:$DEVICE` source may delete this section entirely
      (see the simplification note at bash 516–524) before it's worth porting.
- [ ] `mimeAllowed(mime string) bool` + scan orchestration — the `Mime` stub
      (allowlist/quarantine logic; `clamdscan` itself stays external) (938–999).
- [ ] runtime calc + log export helpers (exit_handler bits, 156–219).

### Bucket B — external but replaceable → do natively in Go

- [x] `staging_avail` (df) → `sysstat.AvailBytes` (`golang.org/x/sys/unix.Statfs`,
      `Bavail * Bsize`, returns bytes). Lives in new `internal/sysstat`.
- [ ] `safe_du` (du) → walk + sum. **Not needed by the rip path** — the capacity
      guard uses `Statfs` (free space) + parsed `Track.Bytes` (need), never a
      recursive dir size. Port only if the librarian (`STAGE_START_SIZE`, ~859) needs
      it. Deferred, not dropped.
- [ ] makemkv beta key fetch (curl) → `net/http` + `regexp` (369–407)
- [ ] ntfy notifications (curl) → `net/http`
- [ ] flock → Go file-lock now; in-daemon mutex later

### Bucket C — genuinely external, stay as `exec` calls

- Streaming: `makemkvcon` (info + per-track `mkv`) is now driven from Go
  (`Info`/`Make`, called by `Disc.Rip`). **`rsync` is gone** — `promote` does the
  staging→permanent copy in Go with a streaming sha256 integrity check on both
  sides. This is ahead of the original "keep streaming in bash" plan; both are now
  Go-owned execs/IO, ready for the daemon to drive directly.
- One-shot, **leave in bash until the daemon lands:** `clamdscan`, `eject`,
  `udevadm` (drive-state / fs probe), `sudo smart_check.sh`.

### Integration — not yet wired

The rip data path exists in Go (`ParseInfo → Rip → verify → promote`) but nothing
invokes it yet: `ripper rip` still `exec`s `media-ripper.sh`, and there's no
`parse-info` command. **Next step to make the ported logic live:** add the command
entry points and cut the bash seam over to them (or, given how much is Go-owned now,
skip straight toward the stage-2 daemon rather than a bash-calls-Go interim).

### Deferred to the daemon step (do NOT convert now)

`write_status` (102–172) and the two live progress loops — `safe_du` polling
during rip (631–635) and rsync `--info=progress2` parsing (863–871) — are coupled
to the `makemkvcon`/`rsync` execs that stay in bash. Converting them now means bash
shelling out to Go every second inside those loops, thrown away once the daemon
owns the exec. **Seam: convert everything except the live progress of the two
streaming commands.**

---

## Design decisions locked in

- **Per-track rip processes.** Rip each selected title to its own subdir
  (`$BATCH_DIR/t$id`) so exactly one mkv lands and the id→file mapping is exact.
  This deletes the entire source-order-mapping guesswork (bash 645–690). Keep the
  rips **sequential** — parallel `makemkvcon` thrashes a single optical drive.
- **Drop the `movie|show` arg.** Infer from arg count: 1 arg (drive) ⇒ movie,
  2 args (drive + season) ⇒ show. `cobra.RangeArgs(1, 2)`.
- **Default track selection = the survivors of the sort filter, decided *before*
  ripping.** Move the anchor/threshold band (697–705) upstream so we only spin the
  disc for tracks we keep. `-t` overrides with an explicit id list.
  - movie: single largest track (`max_by(bytes)`).
  - show: keep tracks where `anchor*0.7 ≤ bytes ≤ anchor*1.3`.
  - **anchor = second-largest size**, not largest — stops a "play-all" title from
    skewing the band.
- **`parse-info` = pure disc inventory** (all tracks), no media/season, no
  selection. One `Disc` JSON blob becomes the single source of truth that both
  bash (`jq`) and later Go stages consume.

## Web UI

- [ ] Replace `<meta http-equiv="refresh" content="1">` with push/patch updates.
      SSE over the existing `net/http` server (fetch-poll `/json` as the quick
      interim). WebSocket only if the browser needs to send commands back.
- [ ] Live system stats on the status page — staging/permanent free space (and
      maybe system RAM). Planned as an in-memory `SysStat` cache in `internal/sysstat`:
      a background goroutine started by `serve` refreshes a mutex-guarded struct on
      an interval; handlers read it; later it's pushed over SSE (same struct). A
      natural precursor to the daemon owning state in memory.
      - **Keep the rip capacity guard on a live `Statfs`, not this cache.** Today
        `rip` and `serve` are separate processes, so the cache isn't visible to the
        rip path at all. Even once the daemon merges them, prefer injecting the
        avail source (a `func() (uint64, error)`) so the guard stays independent of
        the refresher's health and testable standalone.
      - **Mem stat is linux-only:** `unix.Sysinfo` doesn't build on darwin, unlike
        `Statfs`. Needs a `//go:build linux` file + darwin stub when added.
