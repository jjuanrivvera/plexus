# Changelog

## Unreleased

- **The keepalive now reclaims a detached session's inject port** (fixes #1). The v0.5.2
  heartbeat could carry an `inject_port`, but nothing off the busy path re-discovered it: an
  idle `--detach` session never fires PostToolUse, so a session that registered before its edc
  injector bound stayed `inject_port=0`, was invisible to `plexus get`, got no inject, and was
  TTL-pruned. `plexus heartbeat` now re-reads edc's state dir itself — edc names its state file
  `pid-<edcpid>.json`, so plexus matches the one whose live process descends from the session's
  agent pid — and the keepalive passes `--claude-pid` (the agent pid it already tracks) so the
  off-hook heartbeat can find it. The session becomes injectable on the first keepalive tick,
  before the TTL. Port discovery lives in `edc.go` with injectable process seams and unit tests.
  (Auto-provisioning the keepalive timer from `--detach` — suggested fix #3 — is left as a
  separate follow-up: it's OS-specific install surface and belongs with the deploy scripts.)

## v0.5.2

- **A heartbeat can now claim the inject port**, so sessions stop being silently unroutable.
  `inject_port` was only ever written by the SessionStart `register`, but the port is only
  discoverable once the session's injector is up — a session that raced its injector stayed
  `inject_port=0` for its whole life and `plexus get` never routed to it, even though the hooks
  were discovering the port correctly on every heartbeat. `POST /heartbeat` now accepts an
  optional `inject_port`: non-zero claims or updates it, zero leaves the stored port alone (a
  hook that momentarily fails to discover it must not cost the session its injectability).
  Old clients (no field) and old servers (ignore it) both stay compatible.

## v0.5.1

- **Launcher forwards agent flags without `--`**: `plexus claude --dangerously-skip-permissions`
  now reaches the agent instead of failing with `"…" is not a directory`. The first flag plexus
  doesn't own ends plexus's own parsing — like `docker run`, plexus's flags and `[dir]` come first,
  everything after goes to the agent verbatim. The explicit `--` separator still works.

## v0.5.0

**The binary, repo, and module are now `plexus`.** What was the `presence` binary is now
`plexus` — the command, the GitHub repo (`jjuanrivvera/plexus`), the Go module
(`github.com/jjuanrivvera/plexus`), and the config all move to the project name.

- Config moves to `~/.config/plexus/env` with `PLEXUS_*` keys (was `~/.config/presence/env` /
  `PRESENCE_*`); state + DB move to `~/.local/state/plexus/plexus.db`. `plexus.sh` **auto-migrates**
  an existing pre-rename config (renames the keys and moves the file), so the same token keeps
  talking to a registry that hasn't been upgraded yet — the HTTP protocol is unchanged.
- `presence` is kept as a **deprecated back-compat symlink** to `plexus`, so hooks and systemd
  units written before the rename keep resolving during migration.
- Docs move to <https://jjuanrivvera.github.io/plexus/>.

## v0.4.0

**Plexus.** The project is now named **Plexus** — `plexus` (this binary) plus
[`edc`](https://github.com/jjuanrivvera/edc), the two independent tools that let you see, launch,
attach, and inject coding-agent sessions across your machines. The launcher command, the tmux
socket, and the web-terminal auth all move from `mesh` to `plexus`, and the `mesh` symlink is
replaced by `plexus`.

- **`--worktree` / `-w` on the launcher**: `plexus <agent> <repo> --worktree` runs the session in a
  fresh `git worktree` (branch `plexus/<agent>-<suffix>` under `~/.local/state/plexus/worktrees/`),
  so two agents in the same repo never collide on the working tree or the index.
- **One Claude Code plugin for both tools** (`plexus@jjuanrivvera-plexus`): registration + web
  terminal via hooks (`plexus`) and injection via the channel (`edc`), plus the `plexus` skill —
  installed and enabled together.
- **`plexus.sh`**: one command installs both binaries, drops the `plexus` symlink, and scaffolds
  config (generating a registry token and an inject secret) idempotently.
- **LICENSE**: MIT.

Earlier `v0.3.x` tags shipped without changelog notes; they brought the single-sign-in
master-detail cockpit, the ttyd web terminal folded into the binary as `plexus ttyd`, the
`plexus kill` command, and OpenCode launch/attach support.

## v0.2.0

- New session state **`blocked`** (alongside `busy`/`idle`): the session is waiting on human
  input (a permission prompt or a question). It is the highest-signal state — it tells the mesh
  which session needs you right now. `heartbeat --state blocked` and the `/heartbeat` API accept it.
- Hooks: `notification.sh` (Claude Notification → `blocked`) and `user-prompt-submit.sh`
  (UserPromptSubmit → `busy`) to drive the state automatically.

## v0.1.0

First release. Session registry for an ambient agent mesh (Claude Code + Codex).

- HTTP service (`plexus serve`) + client CLI (`register`/`heartbeat`/`deregister`/`list`/`get`/`prune`) —
  one static Go binary, pure-Go SQLite (`CGO_ENABLED=0`).
- Per-agent tracking via the `agent` field (`claude`/`codex`) with `--agent` filters on `list`/`get`;
  an idempotent migration adds the column to existing DBs (backfilling to `claude`).
- Server-side timestamps, TTL auto-prune, constant-time bearer auth, private-address bind only.
- Claude Code session hooks; Codex sessions register through edc's `.codex-plugin`.
