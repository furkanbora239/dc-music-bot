# dc-music-bot

**A self-hosted Discord music bot written in Go.**

YouTube Music search via the local yt-dlp binary; Docker image with a built-in PO-token provider that survives YouTube's bot checks even on datacenter IPs, low memory footprint, and an isolated playback queue per server.

---

## Features

- **Single audio source** — Every search, stream URL and recommendation goes through the local `yt-dlp` binary (no third-party API to go down).
- **Built-in bot-check survival** — A bgutil PO-token provider sidecar ships in `docker-compose.yml`; yt-dlp answers YouTube's "Sign in to confirm you're not a bot" with machine-generated tokens. No accounts, no cookies to babysit.
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
| `YTDLP_POT_BASE_URL` | *set by compose* | Base URL of the bgutil PO-token provider sidecar (e.g. `http://bgutil-provider:4416`). `docker-compose.yml` already wires it; only set it manually when running without compose or on another network. |
| `YTDLP_COOKIES_FILE` | *empty* | Path to a Netscape-format `cookies.txt` for yt-dlp (fallback if PO tokens ever stop being enough) |
| `YTDLP_COOKIES_FROM_BROWSER` | *empty* | Browser name for yt-dlp `--cookies-from-browser` (`chrome`, `firefox`, `safari`) |
| `YTDLP_PROXY` | *empty* | Proxy URL for every yt-dlp request (e.g. `socks5://100.64.0.5:1080`) — alternative escape route from IP blocks |

Invalid or unparsable values silently fall back to their defaults (e.g. an out-of-range `DEFAULT_VOLUME` or a malformed `IDLE_TIMEOUT`).

### YouTube "Sign in to confirm you're not a bot" (datacenter IPs)

YouTube treats datacenter IP ranges as bots. Music apps on a phone or PC look
like normal users and never see the wall — the difference is the egress IP, not
smarter code.

**The Docker setup already contains the fix:** `docker-compose.yml` runs a
[bgutil PO-token provider](https://github.com/Brainicism/bgutil-ytdlp-pot-provider)
sidecar next to the bot, and the image ships the matching yt-dlp plugin. The
provider mints proof-of-origin tokens on the fly (no Google account, no cookies),
so yt-dlp passes the bot check straight from the server IP. Nothing to configure.

Verify it is active:

```bash
docker compose ps                       # bgutil-provider: Up (healthy)
docker compose exec dc-music-bot sh -c 'yt-dlp -v --extractor-args "youtubepot-bgutilhttp:base_url=$YTDLP_POT_BASE_URL" https://www.youtube.com/watch?v=dQw4w9WgXcQ 2>&1 | grep -m1 "PO Token Providers"'
# -> [debug] [youtube] [pot] PO Token Providers: bgutil:http-x.y.z (external), ...
```

> On a non-blocked IP you won't see a `Generating a ... PO Token` line — YouTube
> only demands a token when it challenges the client. The providers line above
> proves the wiring; the generation line will show up on a flagged server.

Keep the stack fresh when YouTube changes the rules (re-pulls yt-dlp **and** the plugin zip):

```bash
docker compose build --build-arg CACHEBUST=$(date +%s) && docker compose up -d
```

**If tokens ever stop being enough**, two fallbacks remain, both off by default:

- `YTDLP_COOKIES_FILE=/path/to/cookies.txt` — Netscape-format cookie file from a logged-in browser. A missing file doesn't crash the bot; it warns and continues as guest. With Docker, add a **writable** bind mount (`:ro` breaks every extraction — yt-dlp writes the refreshed cookie jar back on exit):

  ```yaml
  volumes:
    - ./cookies.txt:/app/cookies.txt   # do NOT add :ro
  ```

- `YTDLP_PROXY=socks5://…` — route only yt-dlp traffic through a residential IP (home machine behind Tailscale, or a proxy service); the Discord gateway connection stays direct.

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
Dockerfile          Multi-stage build (Go) + Alpine runtime image (yt-dlp + POT plugin baked in)
docker-compose.yml  Bot + bgutil-provider sidecar, limits, tmpfs, hardening
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
| `Sign in to confirm you're not a bot` with compose | `bgutil-provider` unhealthy or image stale: `docker compose ps`, then `docker compose build --build-arg CACHEBUST=$(date +%s) && docker compose up -d`. |
| Playback stops after repeated failures | The bot gives up after 3 failed tracks in a row (anti-loop guard). Fix the source (see above) and `/play` again. |

Set `LOG_LEVEL=debug` to see yt-dlp arguments and source fallback transitions in the logs.

---

## License

[MIT](LICENSE)
