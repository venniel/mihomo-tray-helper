# mihomo-go

A lightweight tray controller for Windows that runs the official `mihomo` core in the background and exposes a
small, native control surface: system proxy, TUN mode, mode switching, proxy-group switching and a link to the
Clash dashboard.

The goal is to keep the full power of a normal mihomo config while adding only the Windows-specific glue, without
becoming another all-in-one GUI client.

- Uses the official `mihomo.exe` — nothing is embedded, nothing is bundled
- One YAML config, used as-is
- Native Win32 controls (no GUI toolkit) with explicit accessible names, so screen readers such as NVDA and
  争渡读屏 can navigate the tray menu and settings dialog
- Small idle footprint: no periodic HTTP polling after startup; the Clash API is contacted at startup, when the
  menu is opened and when you switch something

## Features

- Tray icon reflects the current state: TUN active, system proxy enabled, or idle
- Left-click the tray icon to open the settings dialog
- Right-click for the context menu:
    - **更多** (More) — restart, autostart, project page
    - **模式** (Mode) — rule / global / direct (plus script when the config has a script)
    - **节点** (Nodes) — `select` proxy groups; the member currently used by the core is marked, even when it was
      changed from outside this program (for example from the dashboard)
    - **TUN 模式** (TUN mode) — available when the config contains a `tun` section; asks for elevation when needed
    - **系统代理** (System proxy) — toggles the Windows proxy settings
    - **面板** (Dashboard) — opens the Clash dashboard
    - **退出** (Quit) — gracefully stops the core and exits
- The system proxy is restored on the next launch when it was left enabled, and is disabled on normal exit
- TUN state is remembered across restarts; enabling TUN relaunches the program elevated (UAC)
- Single instance per user session; starting a second copy is refused
- Optional autostart through a scheduled task

## Requirements

- Windows 10 or later (64-bit)
- The official `mihomo` for Windows, downloaded from the upstream project:
  <https://github.com/MetaCubeX/mihomo/releases>. The file may keep its download name such as
  `mihomo-windows-amd64-v1.19.33.exe`; select it directly in the settings dialog.
- For TUN mode, a working `wintun.dll` and administrator rights are required
- A mihomo YAML config that satisfies the requirements below

## Getting started

1. Build the program:

   ```sh
   go build -ldflags "-H=windowsgui" -trimpath -o mihomo-go.exe .
   ```

2. Put `mihomo-go.exe` in any folder.
3. Run `mihomo-go.exe`. The included `mihomo-go.ini` points at `mihomo.exe` and `config.yaml` next to the
   program, so the tray icon appears and the core starts immediately. Left-click the tray icon to open the settings
   dialog, where the core executable and the config file are selected as individual files. Delete `mihomo-go.ini`
   if you want the dialog on first launch instead.

The tray menu and the settings dialog are in Chinese.

## mihomo configuration requirements

### Required: a proxy port

The program reads `mixed-port` first, then `port`, then `socks-port`, to configure the Windows system proxy. The
listen address comes from `bind-address`; a wildcard value (`*`, `0.0.0.0`, `::`) is mapped to `127.0.0.1` for
the system proxy.

```yaml
mixed-port: 7890
bind-address: '*'
```

### Optional: tun section

If the config contains a top-level `tun` section, the TUN menu item appears. TUN mode is started by rewriting
`tun.enable` in an in-memory copy of the config; the file on disk is never modified.

```yaml
tun:
  enable: true
  stack: mixed
  auto-route: true
```

When TUN is disabled, the core is started with a temporary copy of the config where `tun.enable` is `false`.

### Optional: Clash API

Group switching, mode switching and the dashboard link use the Clash API:

```yaml
external-controller: '127.0.0.1:9090'
secret: change-me
external-ui: ui # optional
```

If the config contains `select` proxy groups but no `external-controller`, the program starts the core with
`-ext-ctl 127.0.0.1:9090 -secret <generated>` so the API is available for the current run only; the config file is
not rewritten.

### Optional: proxy groups for node switching

Groups with `type: select` are shown in the **节点** menu. The group name is the submenu title and its `proxies`
are the items. The member currently in use is read from `GET /proxies`, so changes made from a dashboard are
reflected as well.

```yaml
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Proxy1, Proxy2]
```

Switching an item sends `PUT /proxies/<group>` to the Clash API and then closes existing connections, so the change
applies immediately.

### Modes

The **模式** menu offers `rule`, `global` and `direct`, plus `script` when the config contains a `script` section.
Selecting a mode sends `PATCH /configs` to the Clash API.

## Program configuration

Settings are stored in `mihomo-go.ini` next to the executable:

```ini
[mihomo-go]
mihomo-core = mihomo.exe
config-file = config.yaml
tun-start-mode = off
system-proxy-auto = 0
; Selector menu layout: "auto", "flat" or "nested"
selector-menu-layout = auto
; Optional: write a log file (relative paths are resolved against the program folder)
; log-file = mihomo-go.log
```

| Key | Description |
| --- | --- |
| `mihomo-core` | Full path to the core executable. It does not need to be named `mihomo.exe`; any filename works. |
| `config-file` | Full path to the mihomo YAML config file (`.yaml` or `.yml`). |
| `tun-start-mode` | TUN state used only on the very first launch: `off` (default) or `on`. Later launches use the remembered state. |
| `system-proxy-auto` | System proxy state used only on the very first launch: `1` enabled, `0` disabled (default). |
| `selector-menu-layout` | Node menu layout: `auto` decides by item count, `flat` shows each group as a disabled header with checkable items below, `nested` uses submenus. |
| `log-file` | Optional log file path. Relative paths are resolved against the program folder. Empty disables logging. |

The settings dialog edits `mihomo-core`, `config-file` and `selector-menu-layout`. Relative paths typed manually
are resolved against the program folder. Saving a different core file or config file restarts the program
automatically.

### State file

Runtime state is kept in `mihomo-go.state.json` next to the executable:

- `tun` — whether TUN was active on the last run
- `system-proxy` — whether the system proxy was enabled on the last run

Delete this file to fall back to `tun-start-mode` and `system-proxy-auto`.

### Autostart

Autostart is implemented as a scheduled task named `mihomo-go` that runs the current executable at logon with the
highest available privileges. Enabling or disabling it may prompt for UAC. If the executable is moved, re-enable
autostart so the task points at the new path.

## Command-line switches

These are mainly used internally when the program relaunches itself, but they can also be passed manually:

| Switch | Effect |
| --- | --- |
| `-tun` | Start with TUN enabled (requests elevation if needed). |
| `-restart` | Wait for an already running instance to exit before starting. |
| `-autostart-enable` | Create the autostart scheduled task. |
| `-autostart-disable` | Remove the autostart scheduled task. |

## How the core is started

The core is started as `<selected core> -f <temporary config> -d <config folder>`. The temporary config is a copy of
the selected YAML file with `tun.enable` forced to the wanted state, so the original file is never modified.
`-d` points at the config folder, so relative provider and rule-set paths keep working and mihomo writes its
`cache.db` and geo databases there.

## Building and testing

Requirements:

- Go 1.27 or newer
- `golang.org/x/sys` and `gopkg.in/yaml.v3` (resolved by `go mod download`)

```sh
go build -ldflags "-H=windowsgui" -trimpath -o mihomo-go.exe .
go test .
```

`-H=windowsgui` suppresses the console window. The UI is written directly against Win32 (GDI/user32/shell32), so the
executable has no runtime dependencies beyond the standard Windows system libraries.

### Source layout

| File | Purpose |
| --- | --- |
| `main.go` | Startup, single instance, DPI/COM initialization, first-run settings, elevation |
| `ui.go` | Tray icon, context menu, system proxy and TUN actions, autostart, shutdown |
| `settings.go` | Win32 settings dialog and file/folder pickers |
| `win32.go` | Win32 bindings, structs, accessibility names, small helpers |
| `drover.go` | Application state, selector/mode state, runtime persistence, shutdown |
| `supervisor.go` | Core process supervision (job object, temporary config, Clash API checks) |
| `apiclient.go` | Minimal Clash API client |
| `config.go` | YAML parsing, requirement checks, TUN rewriting, Clash API detection |
| `options.go`, `appstate.go` | INI options and JSON state file |
| `autostart.go`, `elevation.go`, `singleinstance.go` | Windows integration helpers |
| `systemproxy.go`, `tuncleanup.go`, `logger.go` | System proxy, Wintun cleanup, logging |

## Limitations

- Only one config file at a time; there is no profile switching in the UI.
- Group selections are not stored by this program; mihomo's own `cache.db` decides whether they survive a restart.
- TUN mode requires an elevated process and a `tun` section.
- The program does not modify the mihomo config file; TUN changes are per-run only.
