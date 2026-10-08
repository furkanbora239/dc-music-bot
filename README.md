# 🎵 dc-music-bot

<p align="center">
  <b>A blazingly fast, lightweight, and self-hosted Discord music bot written in Go.</b><br>
  Powered by YouTube Music, Piped API, and yt-dlp. Containerized with Docker.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.24+-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/Discord-Slash%20Commands-5865F2?style=flat&logo=discord" alt="Discord">
  <img src="https://img.shields.io/badge/License-MIT-green.svg" alt="License">
</p>

---

## 🌟 Features / Özellikler

- 🚀 **Blazingly Fast & Lightweight:** Written in Go with minimal resource footprint (< 50MB RAM usage).
- 🎧 **Hybrid Audio Engine:**
  - Inspired by modern clients like **SimpMusic** and **PipePipe**.
  - Fast search and direct stream resolution via **Piped API** (YouTube Music).
  - Built-in fallback to **yt-dlp** for extreme resilience against YouTube IP blocks and bot-detection.
- ⚡ **Discord Slash Commands:** Full Discord Application Command support (`/play`, `/skip`, `/queue`, `/loop`, `/autoplay`, etc.).
- 📻 **Autoplay / Radio Mode:** Automatically queues and plays recommended songs when the queue ends (like Spotify/YouTube Music), with duplicate-prevention history.
- 🎛️ **Interactive UI Buttons:** Control music playback with one click (⏯️ Play/Pause, ⏭️ Skip, ⏹️ Stop, 🔁 Loop, 📻 Autoplay).
- 🛡️ **Guild-Isolated Architecture:** Independent goroutine and playback queue per Discord server.
- ⏱️ **Auto Idle Disconnect:** Automatically disconnects from voice after inactivity (default: 3 minutes) to save CPU and bandwidth.
- 🐳 **Docker & Docker Compose:** Multi-stage minimal container ready for instant self-hosting on any VPS or homelab.

---

## 📋 Slash Commands / Komutlar

| Command | Arguments | Description (EN) | Açıklama (TR) |
|---|---|---|---|
| `/play` | `<query>` | Plays a song or adds to queue (title, artist, or URL) | Şarkı çalar veya kuyruğa ekler (isim, sanatçı ya da link) |
| `/pause` | - | Pauses current audio playback | Çalan müziği duraklatır |
| `/resume` | - | Resumes paused audio | Duraklatılmış müziği devam ettirir |
| `/skip` | - | Skips to the next song in queue | Sıradaki şarkıya geçer |
| `/stop` | - | Stops music, clears queue, leaves voice | Müziği durdurur, kuyruğu temizler ve kanaldan ayrılır |
| `/queue` | - | Displays upcoming songs in the queue | Kuyruktaki şarkı listesini gösterir |
| `/nowplaying` | - | Shows currently playing song with controls | Şu an çalan şarkı bilgilerini ve butonları gösterir |
| `/loop` | `<mode>` | Configures loop mode (`off`, `track`, `queue`) | Tekrar modunu ayarlar (kapalı, tek şarkı, tüm kuyruk) |
| `/shuffle` | - | Randomly shuffles the upcoming playlist | Kuyruğu rastgele karıştırır |
| `/volume` | `<1-100>` | Adjusts audio volume | Ses seviyesini ayarlar |
| `/autoplay` | `[mode]` | Toggles or configures autoplay (`on`, `off`, `status`) | Şarkı bitince önerilenlerle devam etmeyi ayarlar |
| `/help` | - | Lists commands and help documentation | Komut yardım listesini görüntüler |

---

## 🚀 Quick Start (Docker Compose) / Hızlı Başlangıç

### 1. Discord Bot Token Alma
1. [Discord Developer Portal](https://discord.com/developers/applications) adresine gidin.
2. Yeni bir Application oluşturun ve **Bot** sekmesine gelin.
3. **Privileged Gateway Intents** altında:
   - ✅ **Server Members Intent** (opsiyonel)
   - ✅ **Message Content Intent** (gerekmez! Bot slash komutları kullanır)
4. Bot token'ınızı kopyalayın.
5. **OAuth2 -> URL Generator** sekmesinden `bot` ve `applications.commands` seçip, `Connect` ve `Speak` izinleriyle botu sunucunuza davet edin.

### 2. Yapılandırma (`.env`)
Depoyu klonlayın ve `.env` dosyasını oluşturun:
```bash
cp .env.example .env
```
`.env` dosyasını düzenleyip bot tokenınızı girin:
```env
DISCORD_TOKEN=your_bot_token_here
AUDIO_SOURCE_MODE=hybrid
PIPED_API_URL=https://api.piped.private.coffee
DEFAULT_VOLUME=100
IDLE_TIMEOUT=3m
```

### 3. Çalıştırma
```bash
# Docker Compose ile arka planda başlatın
docker compose up -d

# Logları takip etmek için
docker compose logs -f
```

---

## 🛠️ Yerel Geliştirme (Local Development)

### Gereksinimler
- **Go** (1.24+)
- **FFmpeg** (sistem PATH'inde bulunmalı)
- **yt-dlp** (sistem PATH'inde bulunmalı)

```bash
# Bağımlılıkları yükleyin ve derleyin
make build

# Testleri çalıştırın
make test

# Botu çalıştırın
make run
```

---

## ⚙️ Yapılandırma Parametreleri (Configuration)

| Değişken (Variable) | Varsayılan (Default) | Açıklama |
|---|---|---|
| `DISCORD_TOKEN` | *Zorunlu* | Discord Bot Token |
| `DISCORD_APP_ID` | *Otomatik* | Bot Application ID (boş bırakılırsa token'dan algılanır) |
| `DISCORD_GUILD_ID` | *Boş* | Geliştirme sırasında komutların anında yüklenmesi için test sunucusu ID'si |
| `AUDIO_SOURCE_MODE` | `hybrid` | `hybrid` (Piped + yt-dlp fallback), `piped`, veya `ytdlp` |
| `PIPED_API_URL` | `https://api.piped.private.coffee` | Kullanılacak Piped API instance adresi |
| `DEFAULT_VOLUME` | `100` | Başlangıç ses seviyesi (1-100) |
| `IDLE_TIMEOUT` | `3m` | Şarkı bittiğinde boşta bekleme ve kanaldan çıkış süresi |
| `EMPTY_CHANNEL_TIMEOUT` | `1m` | Ses kanalında kullanıcı kalmadığında kanaldan çıkış süresi (`0s` = devre dışı) |
| `DEFAULT_AUTOPLAY` | `false` | Kuyruk bittiğinde otomatik önerilen şarkılarla devam etme modu (`true` veya `false`) |
| `LOG_LEVEL` | `info` | Loglama seviyesi (`info` veya `debug`) |
| `YTDLP_COOKIES_FILE` | *Boş* | yt-dlp için Netscape formatlı `cookies.txt` yolu (bot-duvarı için opsiyonel kurtarma yolu; boş = misafir mod). Docker: `docker-compose.yml` içindeki cookies volume'u aktiftir; **yazılabilir** bırakın (`:ro` eklemeyin) — yt-dlp çıkışta güncellenmiş cookie'leri dosyaya geri yazar, read-only mount her extraction'ı çökertir |
| `YTDLP_COOKIES_FROM_BROWSER` | *Boş* | yt-dlp `--cookies-from-browser` tarayıcı adı (`chrome`, `firefox`, `safari`); `YTDLP_COOKIES_FILE` doluysa o önceliklidir |

> 🍪 **Cookie notu / Cookie note:** Varsayılan davranış **misafir (girişsiz)** moddur; her iki değişken de boşsa yt-dlp komut satırı eskisiyle tamamen aynıdır. Bazı çıkış IP'lerinde YouTube "Sign in to confirm you're not a bot" dediğinde, `YTDLP_COOKIES_FILE` ile bir `cookies.txt` verin ya da `YTDLP_COOKIES_FROM_BROWSER` ile tarayıcı seçin. Dosya bulunamazsa bot çökmez, uyarı loglayıp misafir modda devam eder. The default is guest (no-login) mode with identical yt-dlp args as before; both options are only a rescue path for the bot-detection wall.

---

## 📄 Lisans / License

Bu proje **MIT** lisansı ile lisanslanmıştır. Detaylar için [LICENSE](LICENSE) dosyasına göz atabilirsiniz.
