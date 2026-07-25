# Setup & requirements

## Requirements

The honest dependency picture — separating what's **required** from a particular setup:

| | Required? | Notes |
|---|---|---|
| A machine to run the agent(s) | **Yes** | macOS or Linux |
| Somewhere to run `plexus serve` | **Yes** | any host the agents can reach — including the same machine |
| A private network | Only for **multi-machine** | a VPN (Tailscale, WireGuard, Headscale, ZeroTier), a LAN, or `127.0.0.1` for one machine |
| `tmux` + `ttyd` | Only for **attach / launcher** | the registry + injection work without them; you just lose the clickable terminal |
| A specific VPS / cloud | **No** | any always-on reachable host: a Pi, a home server, a VM, or one of your machines |

**Single machine = zero extra infra.** Run `plexus serve` on `127.0.0.1`, agents register to loopback,
open the cockpit locally. No VPN, no second box.

!!! info "The one networking rule"
    `plexus serve` refuses to listen on `0.0.0.0` — it needs an explicit private address. On one machine
    that's `127.0.0.1`; across machines it's whatever your VPN or LAN assigns. That's why a VPN slots in
    so naturally (a stable private IP per machine), but it's interchangeable.

## Install

Both tools are single static Go binaries with checksum-verified releases.

### Both at once (recommended)

`plexus.sh` installs `plexus` and `edc`, drops a `presence` back-compat symlink (so
pre-rename hooks and units keep resolving), and scaffolds config for both (generating a
registry token and an inject secret the first time):

```sh
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/plexus/main/plexus.sh | sh
```

It never overwrites an existing config, so it's safe to re-run to upgrade.

### One at a time

The two binaries are independent — install only what you need:

```sh
# plexus (registry + cockpit + launcher; also drops the `presence` back-compat symlink)
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/plexus/main/install.sh | sh

# edc (event injection — usable entirely on its own, no plexus required)
curl -fsSL https://raw.githubusercontent.com/jjuanrivvera/edc/main/install.sh | sh
```

`ttyd` (for the web terminal) is a single static binary from its
[releases](https://github.com/tsl0922/ttyd/releases); `tmux` from your package manager.
Neither is needed for `edc` injection on its own.

## One plugin, both tools

Plexus ships **a single Claude Code plugin** that wires both binaries into a session at once:

- **registration** (hooks → `plexus`): the session publishes itself into the registry and
  spawns its web terminal on start, keeps its state fresh, and deregisters on exit;
- **injection** (channel → `edc`): the session gets a local `/inject` endpoint so external
  events arrive as turns.

It also bundles the `plexus` skill. Install and enable it:

```sh
claude plugin marketplace add jjuanrivvera/plexus
claude plugin install plexus@jjuanrivvera-plexus
```

!!! note "Prefer injection only? Use `edc` standalone"
    If you want event injection without the registry/cockpit, install the `edc`-alone plugin instead
    (`claude plugin install event-driven-claude@jjuanrivvera-edc`) and use the matching allowlist
    entry and `--channels` value below. Everything else on this page is identical — just swap the
    `{marketplace, plugin}` pair. See the [`edc` README](https://github.com/jjuanrivvera/edc).

The plugin wires **two things** into a session, and they live in **two different files** — this trips
everyone up, so it's spelled out:

**1. Hooks (registration) → `~/.claude/settings.json`.** The install adds the SessionStart/PostToolUse/
SessionEnd hooks here. This is your normal user settings file.

**2. Channel (injection) → the OS *managed-settings* file (requires admin).** For an *installed*
plugin, Claude Code reads the channel allowlist **only** from managed settings — your
`~/.claude/settings.json` is ignored for this by design, so a process running as you can't
self-authorize an injection channel. Add the plugin to `allowedChannelPlugins` there (with `sudo`):

| OS | managed-settings path |
|---|---|
| macOS | `/Library/Application Support/ClaudeCode/managed-settings.json` |
| Linux | `/etc/claude-code/managed-settings.json` |
| Windows | `C:\ProgramData\ClaudeCode\managed-settings.json` |

```sh
# Linux shown; on macOS use the path above. The value is an ARRAY of {marketplace, plugin} objects.
sudo mkdir -p /etc/claude-code && sudo tee /etc/claude-code/managed-settings.json >/dev/null <<'JSON'
{ "allowedChannelPlugins": [ { "marketplace": "jjuanrivvera-plexus", "plugin": "plexus" } ] }
JSON
```

!!! warning "The one thing that silently breaks injection"
    If the channel allowlist is missing (or you put it in `~/.claude/settings.json`), `/inject` still
    returns `202` but **the turn never arrives** — Claude Code drops it. That `202`-but-nothing-happens
    dead-end is almost always this. Verify with the plugin's MCP log showing `Channel notifications
    registered` (vs `skipped`).

**3. Request the channel per session (every launch).** The allowlist alone isn't enough — each session
must also opt in with `--channels`. `plexus claude` does **not** add this for you; pass it through with
`--`:

```sh
plexus claude ~/code/api -- --channels "plugin:plexus@jjuanrivvera-plexus"
# edc-standalone: --channels "plugin:event-driven-claude@jjuanrivvera-edc"
```

A fresh session prints a `Channels (experimental) … inject directly` line when the capability registered.

!!! note "Codex & OpenCode"
    **Codex** has its own equivalent plugin in the `edc` repo (`.codex-plugin/` — registration
    hooks + the injection adapter). **OpenCode** isn't a plugin: drop
    `edc/.opencode-plugin/plexus.ts` into `~/.config/opencode/plugins/` and launch decoupled
    (see [OpenCode](agents.md#opencode)).

## Configure

Config resolves **flag → env var → `~/.config/plexus/env`** (and `~/.config/edc/config.json` for edc).

```sh
# ~/.config/plexus/env — every machine
PLEXUS_URL=http://<registry-host>:<port>   # the registry (e.g. http://127.0.0.1:8799 for one machine)
PLEXUS_TOKEN=<shared-secret>             # bearer + cockpit login + terminal proxy auth
PLEXUS_HOST=<label>                      # this machine's label, e.g. laptop
```

```json
// ~/.config/edc/config.json — where injection is served
{ "inject_secret": "<secret>", "inject_port": "auto", "inject_bind": "0.0.0.0" }
```

The `edc` binary's own default for `inject_bind`, when the key is absent, is `127.0.0.1` (local
injection only). The sample above is what `plexus.sh` **scaffolds** — `0.0.0.0`, so cross-machine
injection works out of the box. That's safe because the listener is **fail-closed** (no valid secret,
no injection), so the wider bind adds reachability, not trust. For a single machine, or to exclude
other local interfaces, set it to `127.0.0.1` or a specific private IP.

Generate secrets with `openssl rand -hex 32`. Keep them out of version control.

## Run the registry

On the always-on host, run `plexus serve` as a service. A systemd user unit (pulling the token from
the env file `plexus.sh` scaffolded, matching the [plexus README](https://github.com/jjuanrivvera/plexus#deploy-server)):

```ini
[Service]
EnvironmentFile=%h/.config/plexus/env      # PLEXUS_URL / PLEXUS_TOKEN / PLEXUS_HOST
Environment=PLEXUS_BIND=127.0.0.1:8799     # bind address (or a private IP:port for multi-machine)
ExecStart=%h/.local/bin/plexus serve
Restart=always
```

On **macOS** there's no systemd — run `plexus serve` under a launchd LaunchAgent (or any process
manager), with the same env file and command.

Then, on any dev machine, launch an agent and it appears in the cockpit at `PLEXUS_URL/ui`:

```sh
plexus claude ~/code/api
```

## Portability

`plexus` is designed for **one developer with a handful of long-lived sessions across machines they own**.
The design generalizes; the implementation is a solo-to-small-team reference.

| Audience | Fit | What to change |
|---|---|---|
| **Another solo dev** | High | install, set `PLEXUS_URL`/`PLEXUS_TOKEN`, a private net — works as-is |
| **A small team** | Medium | a shared token means everyone sees/controls everything; no per-user identity or audit |
| **A company / product** | Blueprint | the *patterns* transfer; the *code* needs hardening (below) |

**The four things to change to scale up:**

1. **Auth** — replace the shared token with OIDC/SSO, per-user tokens, RBAC, and an audit log.
2. **Registry** — swap single-node SQLite for Postgres + HA + multi-tenancy.
3. **Attach** — `tmux`+`ttyd` assume persistent sessions on machines you own; ephemeral CI/cloud agents
   need a different attach path (or none).
4. **Config** — centralize hook/plugin/policy management instead of per-host installs.

**What transfers at any scale** — and is the real reusable value: the **agent-agnostic registry**, the
**uniform `/inject` contract with one adapter per agent**, and the **trust boundary**. That's the shape an
agent platform needs.
