# ClipLocal

A self-hosted Windows desktop app that replaces Outplayed.gg with **zero telemetry**. ClipLocal runs a background replay buffer, tracks APM locally, lets you save and trim clips, and shares them to Discord via webhook. No Overwolf, no cloud services, no third-party analytics.

---

## Features

| Feature | Detail |
|---|---|
| **Replay buffer** | OBS Studio (portable, bundled) via obs-websocket v5 |
| **Hotkey capture** | Default `F9` — saves a clip instantly |
| **APM tracking** | Rolling 60-second window via Win32 low-level hooks |
| **Clip management** | Gallery UI with thumbnails, duration, game title |
| **Trim editor** | In/out scrubber calling `/clips/:id/trim` |
| **Discord sharing** | One-click webhook post with automatic size downgrade retry |
| **Settings** | Hotkey rebinding, webhook URL, buffer length, storage cap |
| **Always-on-top overlay** | Live APM + recording status, draggable |
| **No telemetry** | Only outbound network call is the Discord webhook |

---

## Architecture

```
cliplocal/
  backend/                  # Go binary — background service & REST API
    main.go                 # HTTP server, wiring, tray icon
    config/                 # YAML config loader
    store/                  # SQLite (modernc pure-Go driver)
    obsws/                  # OBS WebSocket v5 client
    hooks/                  # Win32 keyboard/mouse low-level hooks (APM)
    pipeline/               # fsnotify watcher + ffmpeg encode pipeline
    discord/                # Webhook client with size-retry
    obsconfig/              # Auto-generates OBS profile on first run
    tray/                   # System tray icon (Windows only)
  ui/                       # Tauri 2 + SolidStart (SolidJS) frontend
    src/routes/
      index.tsx             # Clip gallery (main window)
      overlay.tsx           # Always-on-top APM overlay
      settings.tsx          # Settings panel
    src/lib/api.ts          # Typed client for Go backend REST/SSE
    src-tauri/              # Rust/Tauri shell, multi-window
  installer/
    cliplocal.iss           # Inno Setup script
    obs-portable/           # Bundled OBS portable (not in git)
    ffmpeg/                 # Bundled ffmpeg binary (not in git)
  config.example.yaml
```

---

## Quick Start (Development)

### Prerequisites

- Go 1.22+
- Node.js 18+ (for the UI)
- Rust + Cargo (for Tauri)
- OBS Studio portable with obs-websocket v5 enabled
- ffmpeg in PATH (or set `ffmpeg.path` in config)

### 1. Configure

```bash
cp config.example.yaml config.yaml
# Edit config.yaml:
#   obs.path   → path to obs64.exe in your portable OBS
#   obs.password → match your obs-websocket password
#   discord_webhook_url → your Discord webhook URL
```

### 2. Run the backend

```bash
cd backend
go run .
# HTTP server starts on http://localhost:8765
# Health check: curl http://localhost:8765/health
```

### 3. Run the UI (dev mode)

```bash
cd ui
npm install
npm run tauri dev
```

---

## Backend REST API

All endpoints are served on `localhost:8765` only.

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check — `{"ok":true}` |
| `GET` | `/clips` | List all clips (newest first) |
| `POST` | `/clips/:id/trim` | Re-trim clip with `{"in":0, "out":30}` seconds |
| `POST` | `/clips/:id/share` | Post clip to Discord webhook |
| `GET` | `/thumbs/:id` | Serve clip thumbnail image |
| `GET` | `/apm/stream` | SSE stream — `event: apm` every second |
| `GET` | `/config` | Read current config |
| `PUT` | `/config` | Update config (hotkey, webhook URL, etc.) |

---

## Building for Windows

### Backend

```bash
cd backend
GOOS=windows GOARCH=amd64 go build -o cliplocal.exe .
```

### UI (Tauri)

```bash
cd ui
npm run tauri build
# Produces ui/src-tauri/target/release/ClipLocal.exe
```

### Installer

1. Build backend and Tauri app above.
2. Place OBS portable in `installer/obs-portable/`.
3. Place `ffmpeg.exe` in `installer/ffmpeg/`.
4. Compile `installer/cliplocal.iss` with [Inno Setup](https://jrsoftware.org/isinfo.php).

---

## Configuration (`config.yaml`)

```yaml
hotkey: "F9"                              # Replay buffer save hotkey

obs:
  path: "C:\\ClipLocal\\obs-portable\\bin\\64bit\\obs64.exe"
  port: 4455                              # obs-websocket port
  password: "changeme"                    # obs-websocket password

discord_webhook_url: "https://discord.com/api/webhooks/..."

replay_buffer_seconds: 30                 # Length of replay buffer
output_dir: ""                            # Default: %APPDATA%\ClipLocal\clips
max_storage_gb: 10.0                      # Auto-prune oldest unshared clips
port: 8765                                # Local HTTP server port
```

---

## Non-Goals (v1)

- No Overwolf, no browser-engine overlay injection, no third-party SDKs
- No cloud upload/storage — Discord webhook is the only network call
- No per-game event detection (kills/deaths) — APM + manual hotkey only
- No macOS/Linux support in v1

---

## License

MIT
