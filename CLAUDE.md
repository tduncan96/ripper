# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Ripper drives MakeMKV to pull titles off optical discs, sorts them into a
Jellyfin-friendly library, records each run in SQLite, and serves a status web
page. Personal project for one media server — no packaging or portable config.

**Read `ROADMAP.md` before touching rip logic.** The project is mid-migration
from a bash-orchestrated pipeline (`scripts/media-ripper.sh`) to Go
(`internal/makemkv`). Stage-1 logic extraction is essentially done: the Go rip
path drives `makemkvcon` directly and replaced `rsync` with a verified in-Go
copy, so bash is down to a few genuinely-external one-shots (`clamdscan`,
`sudo smart_check.sh`, `eject`) until the Go path is wired into the CLI. The end
goal is a long-lived daemon that owns rip state in memory and pushes to the web UI
over SSE. The ROADMAP has the remaining close-out tasks and the locked-in design
decisions (per-track rip dirs, anchor-band track selection, etc.) — follow them
rather than re-deriving.

## Commands

```sh
go build -o ripper .              # build
golangci-lint run ./...           # lint (CI gate; only gosec is enabled, see .golangci.yml)
git ls-files '*.sh' | xargs shellcheck   # lint bash (CI gate)
go test ./...                     # no tests exist yet
```

CI (`.gitea/workflows/lint-build-deploy.yaml`) runs shellcheck + golangci-lint on
every push, and on `main` cross-compiles (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`)
and scp/ssh-deploys the binary, scripts, and systemd unit to the media server.
The sqlite driver is `modernc.org/sqlite` (pure Go) specifically so `CGO_ENABLED=0`
works.

## Architecture

Cobra CLI (`cmd/`) over a set of `internal/` packages. `main.go` runs a fixed
startup sequence before any command: `prflt.Init()` (load config) →
`db.Init(RipDbPath)` (open sqlite at the config-driven `RIP_DB_PATH`, apply
schema) → `cmd.Execute()`.

- **`internal/prflt`** — config + gating. `ReadConfigFiles` reads every file in
  `/etc/ripper/env/` (`rip.env`, `libr.env`) into the global `MasterConfig`, fills
  defaults, and produces two independent errors (rip side, librarian side).
  Those errors are stored in the global `MasterGate`. **Gating pattern:** each
  command checks `prflt.MasterGate.RipConfig` / `.LibrConfig` at the top of its
  `RunE` and returns it if non-nil — so a misconfigured librarian never blocks
  ripping and vice versa. Config paths (`/etc/ripper/env`, `/usr/local/libexec`)
  are hardcoded constants.

- **`internal/makemkv`** — the ported rip pipeline (the active migration target).
  `ParseInfo` turns `makemkvcon -r info` output into a `Disc` of `Track`s
  (parsing `CINFO`/`TINFO` lines by field number, cleaning the title, sorting by
  disc source order). The data path is complete: `Disc.setDests` resolves
  staging/permanent dirs, `Disc.Rip` auto-selects tracks (largest for a movie, an
  anchor-band around the second-largest for a show; an explicit id list via
  `Disc.SelectTracks` overrides), drives per-track `makemkvcon mkv`, and `verify`s
  each MKV by magic
  bytes + minimum size. Before each track it runs a **per-track staging capacity
  guard** — `sysstat.AvailBytes` vs `Track.Bytes * 11/10` (10% headroom) — and
  fails just that track if staging is short (per-track because `promote` drains
  staging each iteration, so peak use is ~one track; see ROADMAP `capacity` for the
  reservation/eviction logic deliberately left out). `promote` then does the
  staging→permanent move itself — a streaming buffered copy across filesystems with
  a `sha256` integrity check read back on both sides (this replaces the bash
  `rsync`), and runs the same 10%-headroom capacity guard against the permanent
  filesystem first. Parsing lives in `parse.go`; the rip pipeline in `rip.go`.
  `key.go` handles the MakeMKV beta key: `RefreshKey` scrapes the forum page and
  rewrites `~/.MakeMKV/settings.conf`, `KeyExpired` spots the `MSG:5052/5055`
  expired-key codes in info output.
  **Not yet wired:** nothing invokes this path — `ripper rip` still execs
  `media-ripper.sh` (see below). See `ROADMAP.md` for what remains.

- **`internal/sysstat`** — thin OS-stat helpers for the rip/UI layers. `AvailBytes`
  wraps `golang.org/x/sys/unix.Statfs` (`Bavail * Bsize`, returns bytes) for
  free-space checks. `unix` (not stdlib `syscall`, which is frozen) is the
  maintained binding; it's a direct dep as of this work. `DriveState` (`udevadm.go`)
  execs `udevadm info` and checks `ID_CDROM_MEDIA=1` to confirm a disc is present.
  Roadmapped to grow an in-memory `SysStat` cache for the web UI (see ROADMAP Web UI).

- **`internal/db`** — sqlite via a package-global `RipRecordDB *sql.DB` set once
  in `main`. `schema.sql` is `go:embed`ded and applied on every `Init`
  (idempotent). One table, `Runs`. Every CLI invocation opens the DB today; the
  daemon will become the single writer (see ROADMAP).

- **`internal/web`** — `ripper serve` on `:9511`. Routes: `/` (status page),
  `/json` (statuses as JSON), `/records` + `/records/{run_id}` (DB history),
  `/logs/current/{drv}`. Templates and static assets (htmx, logo) are `go:embed`ded.
  The status page currently uses a `<meta refresh>` full-page reload; `/json`
  already exists as the fetch-poll interim for the roadmapped SSE swap.

### The bash↔Go seam (important)

The rip **status JSON file** is the IPC contract between the two worlds. The bash
script writes per-drive status to `STATUS_TMP` (a glob like `/tmp/*.rip-status.json`);
`internal/web` reads and unmarshals those files into `Status` structs to render
the page (`getCurrentStatuses`), and `ripper record` reads one to persist a `Record`
to the DB at rip exit. The `Status` struct's json tags must stay in sync with what
the bash script writes.

`ripper rip` and `ripper lib` don't do the work themselves — they validate args,
then `exec.Command(...).Start()` the corresponding shell script (detached, so it
survives a `serve` restart) passing config as positional args in a fixed order.
If you change the argument order in `cmd/scripts.go`, change it in the script too.

### Conventions to match

- Filesystem access is sandboxed with `os.OpenRoot` + root-relative operations
  (`root.MkdirAll`, `fs.ReadFile(root.FS(), …)`) rather than raw `os` paths —
  keep new filesystem code in that style.
- `exec.Command` calls that shell out carry a `// #nosec G204` comment justifying
  why the input is safe (validated numeric/enum args, trusted constant dirs).
  gosec is the only enabled linter, so preserve these when editing.
