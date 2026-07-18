# Ripper Roadmap

Converting `scripts/media-ripper.sh` from a bash-orchestrated pipeline into Go,
in two stages:

1. **Extract logic (essentially done).** All computation / data-shaping now lives
   in native Go. The rip data path — `ParseInfo → Rip → verify → promote` — drives
   `makemkvcon` directly and replaced `rsync` with a verified in-Go copy. Beta-key
   refresh and the udevadm drive-state probe are Go-owned too. What's left is a
   short list of close-out tasks (below) and then wiring it into the CLI.
2. **Daemon (next).** A long-lived server owns state and runs a goroutine per rip.
   The CLI becomes a thin client that talks to the server over a unix socket. The
   server drives `makemkvcon` via `exec`, holds status in memory, and pushes to the
   web UI over SSE. Bash shrinks to nothing (or near it).

---

## Remaining stage-1 work (close-out tasks)

Roughly in the order to tackle them. Bash line refs point at the behavior being
ported.

1. **Eject (do first).** Port `eject "$DEVICE"` on rip completion/failure. The tool
   stays external; needs a Go caller plus the `-e` flag semantics. (bash 27–30,
   780–784)

2. **Log + info file export.** A small helper in `rip.go` that moves the accumulated
   rip log (the `Rip` `out []byte`) and the disc-info dump to
   `$PERM/$DEST/Logs/$RAW_TITLE.rip.log` and `$RAW_TITLE.info`. Uses `Disc.Raw`
   (now correctly populated). This absorbs the old `INFO_DEST` save and the
   `exit_handler` log export. (bash 45–64, 487–504)

3. **Rip-exit orchestration + record.** Invoke `ripper record` on rip exit and
   finalize status. Runtime calc (elapsed time) is TBD — decide whether it's worth a
   helper. (bash 32–43)

4. **ntfy notifications.** Replace the curl pings with `net/http` POSTs: request
   received, ripping, completion/failure. (bash 24, 309, 610, 625)

5. **CLI track selection.** Wire an explicit track-id list through the CLI into
   `Disc.SelectTracks`, replacing the interactive `-t` prompt loop. (bash 510–546)

6. **Integration / wiring (later).** Add command entry points (`parse-info`, a native
   `rip`) and cut the bash seam over — or skip straight to the stage-2 daemon. Until
   this lands, the Go rip path is unreferenced: `ripper rip` still execs
   `media-ripper.sh`, and `KeyExpired`/`RefreshKey`/`DriveState` are written but
   never called.

### Explicitly not porting (acceptable gaps)

- Preflight checks — block-device test, `ID_FS_TYPE` disc-readable check, the 3×
  drive-detect retry loop (bash 316, 341–357). Whatever didn't come over is fine.
- Episode-filename collision dedup (`_j` suffix, bash 737–743) — the Go
  `offset + order` episode naming is more robust, so a collision guard isn't needed.
- `clamdscan` (mime/virus scan) and `sudo smart_check.sh` — genuinely external,
  stay in bash, not invoked from the Go path.

### Dropped (were in the bash script; deliberately not ported)

- MakeMKV index resolution (`DRV:` → `disc:N`) — the `dev:$DEVICE` source addresses
  the device directly, deleting the whole section.
- `mimeAllowed` + scan orchestration — no longer part of the pipeline.
- flock file locking — contention is handled by in-process checks.
- `reserved_by_others` / `sibling_in_starting` cross-process contention — becomes
  the daemon's in-memory job map + mutex.
- source-order mapping — per-track rip dirs make the id→file mapping exact.

### Deferred to the daemon (do NOT convert now)

`write_status` (bash 102–172) and the two live progress loops — `safe_du` polling
during rip (631–635) and rsync `--info=progress2` parsing (863–871) — are coupled to
the exec loop the daemon will own. Converting them now means bash shelling out to Go
every second, thrown away once the daemon owns the exec. `on_signal` (70–83) becomes
in-process context cancellation.

---

## Already ported (reference)

| Concern | Bash | Go |
|---|---|---|
| TINFO/CINFO track parsing | 435–460 | `ParseInfo` (`parse.go`) |
| title cleaning (strip SEASON/DISC, titlecase) | 409–411 | `ParseInfo` |
| dest-path derivation (Movie / Show/Season N) | 415–427 | `Disc.setDests` |
| track selection — movie largest / show anchor-band | 548–562, 697–705 | `Disc.Rip` |
| episode offset numbering | 707–744 | `Disc.Rip` |
| per-track `makemkvcon mkv` (sequential, per-track dirs) | 615–628 | `Make` + `Disc.Rip` |
| `makemkvcon info` | 395 | `Info` (`dev:` source) |
| capacity guard (per-track, 10% headroom) | 578–603 | `Disc.Rip` / `promote` |
| `rsync` promote → in-Go copy + sha256 both sides | 863–871 | `promote` |
| `staging_avail` (df) | 98–100 | `sysstat.AvailBytes` |
| `drive_state` (udevadm) | 85–88 | `sysstat.DriveState` |
| beta key fetch/apply/expired detect | 218–256 | `key.go` (`RefreshKey`/`KeyExpired`) |
| batch-dir cleanup | 777 | `staging.RemoveAll` |

**Behavior change, by design:** show ripping no longer produces an `Extras`/`.review`
split. Bash ripped *all* show tracks then sorted small→`Extras`, oversize→`.review`.
Go selects only the anchor band (0.7–1.3× the second-largest) *before* ripping, so
out-of-band tracks are never ripped. This is the locked-in "select survivors
upstream" decision — you lose the small bonus tracks entirely.

---

## Target architecture (stage 2 — daemon)

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
