# dc-music-bot

**A self-hosted Discord music bot written in Go.**

YouTube Music search via the local yt-dlp binary; Docker image, low memory footprint, and an isolated playback queue per server.

---

## Features

- **Single audio source** — Every search, stream URL and recommendation goes through the local `yt-dlp` binary (no third-party API to go down).
- **Slash commands** — `/play`, `/skip`, `/queue`, `/loop`, `/volume`, `/autoplay` and more.
- **Playlist support** — Passing a playlist URL enqueues up to 100 tracks.
- **Autoplay (radio mode)** — When the queue runs out, plays recommendations from YouTube's `RD` radio mix; tracks already played are not suggested again (last 50 tracked).
- **Interactive buttons** — Play/Pause, Skip, Stop, Loop and Autoplay with a single click.
- **Per-guild isolation** — A separate goroutine and queue for each guild; one server never affects another.
- **Auto disconnect** — Leaves the voice channel when the queue empties or nobody is left in the channel (both durations configurable).
- **Docker ready** — Multi-stage minimal Alpine image, non-root user, memory/CPU limits.

---

## Commands

| Command | Argument | Description |
|---|---|---|
| `/play` | `query` (required) | Song title, artist, or direct URL (video or playlist) |
| `/pause` | — | Pauses the current track |
| `/resume` | — | Resumes the paused track |
| `/skip` | — | Skips to the next track |
| `/stop` | — | Stops playback, clears the queue, leaves the voice channel |
| `/queue` | — | Lists upcoming tracks |
| `/nowplaying` | — | Current track info + control buttons |
| `/loop` | `mode` | `off` · `track` (repeat one) · `queue` (repeat all) |
| `/shuffle` | — | Randomly shuffles the upcoming queue |
| `/volume` | `level` (1-100) | Adjusts playback volume |
| `/autoplay` | `mode` (optional) | `on` · `off` · `status`; without an argument it toggles |
| `/help` | — | Command list |

> **Note:** You must be in a voice channel when running a command; the bot joins that channel.

---

## Setup

### 1) Create the Discord bot

1. Go to [Discord Developer Portal](https://discord.com/developers/applications) → **New Application**.
2. Copy the token from the **Bot** tab.
3. **Privileged Gateway Intents are not required** — the bot only uses the `Guilds` and `Guild Voice States` intents.
4. **OAuth2 → URL Generator** → scopes: `bot` + `applications.commands`; bot permissions: `Connect`, `Speak`. Invite the bot with the generated URL.

### 2) Configure

```bash
git clone https://github.com/user/dc-music-bot.git
cd dc-music-bot
cp .env.example .env
```

Fill in at least the token in `.env`:

```env
DISCORD_TOKEN=your_bot_token_here
```

### 3) Run (Docker)

```bash
docker compose up -d          # start in the background
docker compose logs -f        # follow logs
docker compose down           # stop
```

On first startup slash commands are registered globally, so they may take a few minutes to appear in Discord.
Set `DISCORD_GUILD_ID` during development to register commands to a single server **instantly**.

---

## Local development

Requirements: **Go 1.26+**, **FFmpeg**, **yt-dlp** (both must be on `PATH`).

```bash
make build   # compile to ./bin/dc-music-bot
make run     # go run ./cmd/bot
make test    # go test -v -race ./...
make clean   # remove build artifacts
```

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `DISCORD_TOKEN` | *required* | Discord bot token |
| `DISCORD_APP_ID` | *auto* | Detected from the session when left empty |
| `DISCORD_GUILD_ID` | *empty* | Test server ID for instant command registration |
| `DEFAULT_VOLUME` | `100` | Initial volume (1-100) |
| `IDLE_TIMEOUT` | `3m` | Idle wait duration after the queue empties |
| `EMPTY_CHANNEL_TIMEOUT` | `1m` | Leave duration when the voice channel is empty (`0s` disables) |
| `DEFAULT_AUTOPLAY` | `false` | Keep playing recommendations when the queue ends |
| `LOG_LEVEL` | `info` | `info` or `debug` |
| `YTDLP_COOKIES_FILE` | *empty* | Path to a Netscape-format `cookies.txt` for yt-dlp |
| `YTDLP_COOKIES_FROM_BROWSER` | *empty* | Browser name for yt-dlp `--cookies-from-browser` (`chrome`, `firefox`, `safari`) |

Invalid or unparsable values silently fall back to their defaults (e.g. an out-of-range `DEFAULT_VOLUME` or a malformed `IDLE_TIMEOUT`).

### YouTube "Sign in to confirm you're not a bot" wall

Cookie options are **off by default** (guest mode); yt-dlp arguments remain unchanged.
Some egress IPs trigger a YouTube check — in that case enable one of the two rescue paths:

- `YTDLP_COOKIES_FILE=/path/to/cookies.txt` — Netscape-format cookie file. If `YTDLP_COOKIES_FROM_BROWSER` is also set, the **file wins**.
- `YTDLP_COOKIES_FROM_BROWSER=chrome` — Read cookies from the browser automatically.

A missing file does not crash the bot: it logs a warning and continues in guest mode.

If you use cookies with Docker, add a **writable** volume to the compose file:

```yaml
volumes:
  - ./cookies.txt:/app/cookies.txt   # do NOT add :ro
```

Adding `:ro` breaks every extraction, because yt-dlp writes its refreshed cookie jar back to the file on exit.
`.gitignore` already covers `cookies.txt` — never commit it.

---

## Project structure

```
cmd/bot/            Application entry point (main.go)
internal/
  audio/            Track model + yt-dlp source
    ytdlp.go        yt-dlp process management, search, stream resolving, recommendations
  bot/              Discord layer
    bot.go          Session, intents, slash command registration
    commands.go     Command definitions
    handlers.go     Command and button interactions
  config/           Environment variable loading and validation
  dca/              FFmpeg → Opus encoding and streaming to Discord (trimmed vendored copy of jonas747/dca)
  player/           Per-guild player, queue, loop/autoplay logic
Dockerfile          Multi-stage build (Go) + Alpine runtime image
docker-compose.yml  Resource limits, tmpfs, security hardening
Makefile            build / run / test / docker-* targets
```

Data flow: a user runs `/play` → `ytdlp.go` finds the track → `player` adds it to the queue → `dca` encodes it to Opus with FFmpeg and streams it to the Discord voice channel.

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Commands not showing in Discord | Global registration takes a few minutes. Try instant registration with `DISCORD_GUILD_ID`. |
| `DISCORD_TOKEN environment variable is required` | `.env` is missing or `DISCORD_TOKEN` is empty. |
| `yt-dlp` / `ffmpeg` not found | The binaries are not on `PATH` (they ship with the Docker image). |
| Bot does not join the channel | Check that `Connect` and `Speak` permissions are granted for that channel. |
| Tracks fail to play, bot-detection error | Enable a cookie rescue path (see above). |

Set `LOG_LEVEL=debug` to see yt-dlp arguments and source fallback transitions in the logs.

---

## License

[MIT](LICENSE)
