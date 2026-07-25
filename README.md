# plexus

**Plexus — the eyes and hands.** One Go binary (installed as `plexus`) that does three
things for coding-agent sessions — Claude Code, Codex, or OpenCode — across your machines:

- a **registry** — every session publishes its live state (repo, agent, state, inject port, attach
  address) to a tiny SQLite-backed HTTP service;
- a **cockpit** — a web/PWA dashboard with each session's live terminal (view, type, interrupt);
- a **launcher** — `plexus claude|codex|opencode [dir]` starts an attachable session in tmux.

It pairs with **[edc](https://github.com/jjuanrivvera/edc)**, which injects external events into those
sessions as turns. Together they are **Plexus**.

> 📖 **Full documentation → <https://jjuanrivvera.github.io/plexus/>**

The registry is a read-mostly blackboard (choreography, not orchestration): sessions write their own row,
a reader queries and decides.

## Quick start (one machine)

```sh
# 1. install both binaries + scaffold config (generates a token and an inject secret)
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/plexus/main/plexus.sh | sh

# 2. plexus.sh already wrote ~/.config/plexus/env with a generated PLEXUS_TOKEN — check it
cat ~/.config/plexus/env        # PLEXUS_URL=http://127.0.0.1:8799, PLEXUS_TOKEN=…, PLEXUS_HOST=…

# 3. run the registry (fails closed without PLEXUS_TOKEN, which the env file supplies)
plexus serve --bind 127.0.0.1:8799 &

# 4. launch an agent into the cockpit
plexus claude ~/code/api

# 5. open the cockpit (xdg-open on Linux)
open "$PLEXUS_URL/ui"
```

That gets you *see + launch + attach* on one box. Turning a session **injectable** (the channel
plugin + a machine-level allowlist) and going multi-machine are in the
[full docs](https://jjuanrivvera.github.io/plexus/). Prefer to install just this binary (no `edc`,
no config scaffolding)? See [Install](#install) below.

## Architecture

```
   machine A ──register/heartbeat (HTTP, over a private network)──┐
   machine B ──register/heartbeat (HTTP, over a private network)──┤
                                                                   ▼
                     server:  plexus serve  →  SQLite (~/.local/state/plexus/plexus.db)
                                                                   ▲
                     router  ── plexus get/list ─────────────────┘
```

One static Go binary (pure-Go SQLite via `modernc.org/sqlite`, `CGO_ENABLED=0`):

- `plexus serve` — the service. Binds an explicit private address only, never `0.0.0.0`
  (a private overlay such as Tailscale/WireGuard is the intended perimeter). Stamps all
  timestamps server-side (no clock skew) and auto-prunes rows older than the TTL (default `300s`).
- `plexus register/heartbeat/deregister/list/get/prune/version` — the client, run from any
  machine, typically via agent session hooks.

## Agents

Each row carries an `agent` field (`claude` by default, `codex`, or any future agent). It lets a
router route and dedup **per agent** — "is there already a Codex session on this repo?" — without
overloading the session id.

- **Claude Code** sessions register via the hooks in `hooks/` (see below): the session-start hook
  runs `plexus ttyd spawn` + `plexus register` (`agent=claude`), session-end deregisters.
- **Codex** sessions register via the [`edc`](https://github.com/jjuanrivvera/edc) `.codex-plugin`
  hooks (`agent=codex`). Interactive Codex sessions register with `inject_port=0` (visible but not
  an injection target); the `edc codex serve` daemon registers with a real inject port.
- **OpenCode** sessions register via an OpenCode plugin (ships in
  [`edc`](https://github.com/jjuanrivvera/edc) at `.opencode-plugin/plexus.ts`, installed to
  `~/.config/opencode/plugins/`) on the `session.created` event (`agent=opencode`) — the same
  `plexus ttyd spawn` + `plexus register` wiring. `plexus opencode [dir]` launches a **decoupled
  stack** — an addressable `opencode serve` + a TUI-mode `edc` sidecar + the `opencode attach` the
  human sees — so the interactive session is both **attachable and injectable** (`edc /inject`
  events land visibly in the TUI). `edc opencode serve` alone is the standalone headless daemon.

Every agent goes through the same two calls — `plexus register` (identity + inject port +
attach address) and `plexus ttyd spawn` (the web terminal the cockpit attaches) — only *where*
they are wired differs (Claude/Codex hooks, an OpenCode plugin). That is what makes the cockpit
agent-agnostic.

Register with `--agent codex` (or `$PLEXUS_AGENT`); filter with `list --agent` / `get --agent`.
An empty agent defaults to `claude` server-side, so pre-agent clients keep working unchanged.

## API

All routes except `/healthz` require `Authorization: Bearer $PLEXUS_TOKEN` (constant-time
compare). Bodies are JSON, capped at 16 KiB. Errors are `{"ok":false,"error":"..."}` with
400/401/404/405/500.

| Method · route | Body / query | Effect |
|---|---|---|
| `POST /register` | `{session_id, host, repo, repo_path, branch, inject_port, pid, agent?}` | Upsert; `started_at` set on first insert, `last_seen` always; state forced `busy`; `agent` defaults `claude` |
| `POST /heartbeat` | `{session_id, state?}` | Bump `last_seen`; `state` defaults `busy`; **404** if unknown |
| `POST /deregister` | `{session_id}` | Delete row (idempotent) |
| `GET /list?host=&repo=&agent=&fresh=` | — | Live rows; exact filters; `fresh` Go duration, default `120s` |
| `GET /get?repo=&host=&agent=&fresh=` | — | Delegation query: freshest row matching `repo` (required) + `host` (optional CSV = OR) + `agent` (optional) with `inject_port>0`; `200 {row}` or `204` |
| `POST /prune` | `{older_than?}` | Delete rows older than `older_than` (default: server TTL); returns `{ok, pruned}` |
| `GET /healthz` | — | Liveness, no auth, `text/plain "ok"` |
| `GET /ui` | — | Live dashboard (static HTML, no auth — its JS calls `/list` with the token, asked once and kept in localStorage) |

Ties in `/get` break by `last_seen` DESC then `session_id` ASC (deterministic).

```bash
curl -s -H "Authorization: Bearer $PLEXUS_TOKEN" -X POST $PLEXUS_URL/register \
  -d '{"session_id":"abc-123","host":"laptop","repo":"myrepo","repo_path":"/path/to/myrepo","branch":"main","inject_port":8801,"pid":4242,"agent":"codex"}'
# → {"ok":true}

curl -s -H "Authorization: Bearer $PLEXUS_TOKEN" "$PLEXUS_URL/get?repo=myrepo&agent=codex"
# → 200 {"session_id":"abc-123",...,"agent":"codex","inject_port":8801,...}   (injectable match)
# → 204                                                                       (none)
```

## CLI

```
plexus serve      [--bind ADDR] [--db PATH] [--ttl 300s]
plexus register   [--session-id ID] [--inject-port N] [--host LABEL] [--agent claude|codex]
plexus heartbeat  [--session-id ID] [--state busy|idle]
plexus deregister [--session-id ID]
plexus list       [--host H] [--repo R] [--agent A] [--fresh 2m] [-o json|table]
plexus get        --repo R [--host a,b] [--agent A] [-o json]   # exit 0 = match, 1 = none, 2 = error
plexus prune      [--older-than 10m]
plexus version
```

- `register` auto-detects: session id (`$CLAUDE_SESSION_ID` or `--session-id`), host label
  (`$PLEXUS_HOST`, fallback = hostname lowercased and truncated at the first dot), repo/branch
  from the launch cwd via git (empty outside a repo), inject port (`--inject-port` or
  `$EDC_INJECT_PORT`, 0 = not injectable), agent (`--agent` or `$PLEXUS_AGENT`, else `claude`).
  Repo info is captured **once at register** — a session belongs to the dir it opened in;
  `cd`/`checkout` mid-session are not tracked by design.
- A `heartbeat` that gets a **404** (server pruned the row, e.g. the machine slept past the TTL)
  re-registers automatically and retries once.
- Client HTTP timeout is 2s so hooks can never hang a session.
- Exit codes: `0` success, `1` no match (`get` only), `2` network/auth/server error.

## Config

Precedence: **flag > env var > `~/.config/plexus/env`**. The env-file is read by the binary
itself (`KEY=VALUE` lines, `#` comments) — hooks don't depend on your shell sourcing anything.

```sh
# ~/.config/plexus/env (all machines)
PLEXUS_URL=http://<server-address>:8799
PLEXUS_TOKEN=<shared Plexus secret>
PLEXUS_HOST=<this-machine-label>
PLEXUS_AGENT=claude          # or codex, per the agent running on this machine
# server only:
PLEXUS_BIND=<private-address>:8799   # an explicit private address, never 0.0.0.0
PLEXUS_TTL=300s
```

The token is a shared secret of Plexus (the private overlay is the perimeter); it lives on each
machine, never in the store. `serve` fails closed if the token is unset.

## Claude Code hooks

Copy `hooks/*.sh` somewhere stable (e.g. `~/.claude/hooks/plexus/`), make them executable, and
add to `settings.json`:

```json
{
  "hooks": {
    "SessionStart": [ { "hooks": [ { "type": "command", "command": "~/.claude/hooks/plexus/session-start.sh" } ] } ],
    "PostToolUse":  [ { "hooks": [ { "type": "command", "command": "~/.claude/hooks/plexus/post-tool-use.sh" } ] } ],
    "SessionEnd":   [ { "hooks": [ { "type": "command", "command": "~/.claude/hooks/plexus/session-end.sh" } ] } ]
  }
}
```

- `session-start.sh` → `plexus register` + persists `session → pid, cwd, port` under
  `~/.local/state/plexus/sessions/` (consumed by keepalive).
- `post-tool-use.sh` → `plexus heartbeat`, throttled to ≤1/min via `~/.local/state/plexus/last-hb`.
- `session-end.sh` → `plexus deregister` + cleans the mapping.

All three are silent and fail-soft: if the server is unreachable they no-op within 2 seconds and
never break the session. Codex sessions are wired equivalently through `edc`'s `.codex-plugin`
(SessionStart/PreToolUse/Stop → register/heartbeat with `--agent codex`).

## Keepalive (idle long-lived sessions)

`PostToolUse` heartbeats only fire while a session is using tools; a long-lived session that waits
idle for events would age out of the registry at the TTL. `hooks/keepalive.sh` — installed as a
periodic timer (e.g. a systemd user timer, every 60s) — sends `--state idle` for each mapped
session whose process is still alive, and deregisters the dead ones (covering sessions that die
without a clean end hook). Because heartbeat re-registers on 404, a pruned-but-alive session
recovers on its next tick.

## Install

Most people want **both** binaries plus config scaffolding — use `plexus.sh` (see
[Quick start](#quick-start-one-machine)). To install **just this binary** (no `edc`, no config
written), from a GitHub release (checksum-verified, no Go needed):

```sh
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/plexus/main/install.sh | sh
```

This also drops a `presence` back-compat symlink so pre-rename hooks and units keep resolving.
From source: `go build -o plexus .` (Go 1.25+, `CGO_ENABLED=0`).

## Claude Code skill

The repo ships a `plexus` skill (`skills/plexus/SKILL.md`) that teaches an agent to
drive Plexus — list/launch/attach/kill sessions, the cockpit, scripting the
registry. Install it into a project (or globally) with the skills manager:

```sh
npx skills add jjuanrivvera/plexus            # into this project's .claude/skills
npx skills add jjuanrivvera/plexus --global   # for all projects
```

## Deploy (server)

systemd user unit at `~/.config/systemd/user/plexus.service`:

```ini
[Unit]
Description=plexus — Plexus session registry
After=network-online.target

[Service]
EnvironmentFile=%h/.config/plexus/env
ExecStart=%h/.local/bin/plexus serve
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
```

DB lives at `~/.local/state/plexus/plexus.db` (WAL + busy_timeout). The `agent` column is
added by an idempotent migration on open, so upgrading an existing DB is safe (existing rows
backfill to `claude`).
