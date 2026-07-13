# Ripper Roadmap

Converting `scripts/media-ripper.sh` from a bash-orchestrated pipeline into Go,
in two stages:

1. **This pass — extract logic.** Move all computation / data-shaping out of bash
   into native Go functions. Bash keeps orchestration plus the two streaming
   hardware commands (`makemkvcon`, `rsync`). One-shot external tools stay in bash
   for now.
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

- [ ] `ParseInfo(r io.Reader) (Disc, error)` — track parsing (bash 435–504)
- [ ] `CleanTitle(raw string) string` — strip SEASON/DISC, titlecase (409–411)
- [ ] `DestRel(title string, season int) string` — season 0 ⇒ movie (415–418)
- [ ] `Select(tracks []Track, opts) ([]int, totalMB)` — default anchor-band /
      largest, `-t` override, size estimate (509–564)
- [ ] `contention` over `[]Status` — `reserved_by_others` + `sibling_in_starting`
      (174–216)
- [ ] `capacity` — need-vs-avail math (578–603)
- [ ] index resolution — parse `DRV:` lines → device↔disc index (381–405)
- [ ] `mimeAllowed(mime string) bool` — scan allowlist (787–809)
- [ ] `PlanRename(files, tracks, season, offset) []Move` — sort/episode naming
      (694–775); now trivial thanks to per-track rip dirs
- [ ] runtime calc + log export helpers (exit_handler bits)

### Bucket B — external but replaceable → do natively in Go

- [ ] `staging_avail` (df) → `syscall.Statfs`
- [ ] `safe_du` (du) → walk + sum, or `Statfs` delta
- [ ] makemkv beta key fetch (curl) → `net/http` + `regexp` (219–256)
- [ ] ntfy notifications (curl) → `net/http`
- [ ] flock → Go file-lock now; in-daemon mutex later

### Bucket C — genuinely external, stay as `exec` calls

- Streaming, **stay in bash this pass:** `makemkvcon` (info + per-track `mkv`), `rsync`.
- One-shot, **leave in bash until the daemon lands:** `clamdscan`, `eject`,
  `udevadm` (drive-state / fs probe), `sudo smart_check.sh`.

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
