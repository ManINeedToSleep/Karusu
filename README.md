<div align="center">

```
    ██╗  ██╗ █████╗ ██████╗  █████╗ ███████╗██╗   ██╗
    ██║ ██╔╝██╔══██╗██╔══██╗██╔══██╗██╔════╝██║   ██║
    █████╔╝ ███████║██████╔╝███████║███████╗██║   ██║
    ██╔═██╗ ██╔══██║██╔══██╗██╔══██║╚════██║██║   ██║
    ██║  ██╗██║  ██║██║  ██║██║  ██║███████║╚██████╔╝
    ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝ ╚═════╝
```

**烏 — the crow that hunts your music**

*A self-hosted music manager and Lidarr alternative — built in Go, powered by Soulseek.*

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?style=flat-square&logo=postgresql)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)
![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)

</div>

---

**Karasu** is an open-source, self-hosted music library manager built for homelabs. It monitors artists, automatically searches and downloads music via Soulseek (using [slskd](https://github.com/slskd/slskd)), fetches metadata from MusicBrainz, organizes files into a clean folder structure, and writes ID3 tags — all without lifting a finger.

Built as a Lidarr replacement. Lidarr is currently unmaintained and broken — Karasu picks up where it left off, with a cleaner codebase, smarter result scoring, and first-class Soulseek support. Written in Go for low memory usage, fast file I/O, and a single deployable Docker binary.

---

## How it works

```
You add an artist
       ↓
Karasu fetches their full discography from MusicBrainz
Fanart.tv pulls the artist image automatically
       ↓
Every album is queued as "wanted"
       ↓
Karasu searches Soulseek via slskd
Scores every result: FLAC > MP3, bitrate, completeness, uploader speed
Downloads the best match
       ↓
Organizes files: /music/Artist/Album (Year)/01 - Track.flac
Writes ID3 tags: title, artist, album, genres, track numbers
Updates the database, marks album as downloaded
       ↓
Monitor runs every 24h — new releases are picked up automatically
```

Everything runs in the background. Add an artist, walk away, come back to music.

---

## Features

- **Automatic Soulseek downloads** — Integrates with [slskd](https://github.com/slskd/slskd) to search and download music from the Soulseek P2P network
- **MusicBrainz metadata** — Searches the MusicBrainz database for artists, imports full discographies with release dates and album types (Album, EP, Single, Live, Compilation)
- **Fanart.tv artist images** — Fetches high-quality artist images automatically on import, keyed by MusicBrainz ID
- **Intelligent result scoring** — Prefers FLAC over MP3, higher bitrates, 24-bit depth, complete albums, and fast uploaders. Penalizes uploaders with no free slots rather than skipping them entirely
- **Wildcard search retry** — If a Soulseek search returns nothing, retries with a wildcard query to bypass common filters
- **24-hour release monitor** — Polls all monitored artists for new MusicBrainz releases every 24 hours and auto-queues them for download
- **Library scanner** — Reconciles existing files on disk with the database, matching by ID3 tags or filename — useful if you're migrating from another tool
- **Automatic file organization** — Moves downloaded files into a standardized folder and filename structure
- **ID3 tag writing** — Writes complete metadata tags (title, artist, album, year, genres, track/disc number) so every player reads them correctly
- **Per-album status tracking** — Every album has a status: `wanted` / `downloading` / `downloaded` / `missing`
- **REST API** — Full JSON API designed to back a frontend like [Melodix](https://github.com/ManINeedToSleep/Melodix)
- **Zero-config migrations** — Database schema is applied automatically on startup via golang-migrate
- **Docker-ready** — Statically compiled, single binary, minimal Alpine image

---

## Why not Lidarr?

Lidarr is effectively dead — no active development, persistent bugs, and a plugin ecosystem that's breaking down. Karasu is built from scratch to do the same job better:

| | Karasu | Lidarr |
|---|---|---|
| Actively maintained | ✅ | ❌ |
| Soulseek support | ✅ Native via slskd | ⚠️ Plugin (broken) |
| Result quality scoring | ✅ FLAC/bitrate/speed/completeness | ⚠️ Basic |
| Memory footprint | ✅ Low (Go binary) | ❌ Heavy (.NET) |
| Single binary deploy | ✅ | ❌ |
| Self-hostable | ✅ | ✅ |

---

## Tech stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| HTTP framework | Gin |
| Database | PostgreSQL + sqlx |
| Migrations | golang-migrate |
| Soulseek client | slskd (REST API) |
| Music metadata | MusicBrainz API |
| Artist images | Fanart.tv API |
| Tag writing | bogem/id3v2 |
| Tag reading | dhowden/tag |
| Config | godotenv |

---

## Project structure

```
karasu/
├── cmd/
│   └── karasu/
│       └── main.go                  # Entrypoint — wires everything together
├── internal/
│   ├── api/
│   │   ├── handlers.go              # HTTP handlers for all routes
│   │   └── helpers.go               # Date parsing, album type normalization
│   ├── db/
│   │   ├── db.go                    # Connection + migration runner
│   │   ├── repository.go            # All database queries (artists, albums, tracks, genres)
│   │   └── migrations/
│   │       ├── 001_initial.up.sql
│   │       └── 001_initial.down.sql
│   ├── library/
│   │   ├── downloader.go            # Full download pipeline: search → score → fetch → organize
│   │   ├── helpers.go               # Shared helpers (date parsing, album type normalization)
│   │   ├── monitor.go               # 24h release monitor — auto-queues new albums
│   │   ├── organizer.go             # File moving, folder structure, tag writing
│   │   └── scanner.go               # Reconciles files on disk with the database
│   ├── metadata/
│   │   ├── fanart.go                # Fanart.tv client — artist images via MusicBrainz ID
│   │   └── musicbrainz.go           # MusicBrainz API client (rate-limited to 1 req/s)
│   ├── models/
│   │   └── models.go                # Artist, Album, Track, Genre structs and status enums
│   └── slskd/
│       ├── client.go                # slskd REST API client (search, download, status)
│       └── client_test.go
├── Dockerfile
├── .env.example
└── go.mod
```

---

## API reference

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Liveness check |
| `GET` | `/api/search?q=` | Search MusicBrainz for artists by name |
| `GET` | `/api/artists` | List all monitored artists |
| `POST` | `/api/artists` | Add an artist — fetches image and syncs full discography |
| `GET` | `/api/artists/:id` | Artist detail with albums and tracks |
| `DELETE` | `/api/artists/:id` | Remove artist and cascade all data |
| `PUT` | `/api/artists/:id/monitored` | Toggle artist monitoring on/off |
| `GET` | `/api/albums/:id` | Album detail with full track listing |
| `PUT` | `/api/albums/:id/download` | Trigger Soulseek download for a specific album |
| `GET` | `/api/library/wanted` | All albums with `wanted` status |
| `POST` | `/api/library/scan` | Scan music directory and reconcile files with database |

---

## Getting started

### Prerequisites

- [slskd](https://github.com/slskd/slskd) — running Soulseek daemon with REST API enabled
- PostgreSQL 14+
- A free [Fanart.tv](https://fanart.tv) personal API key
- Docker (recommended) or Go 1.22+

### Docker (recommended)

```bash
cp .env.example .env
# Fill in your values, then:
docker build -t karasu .
docker run --env-file .env -p 8080:8080 -v /mnt/music:/mnt/music karasu
```

### Environment variables

```env
DB_HOST=postgres
DB_PORT=5432
DB_USER=karasu
DB_PASSWORD=yourpassword
DB_NAME=karasu

SLSKD_URL=http://slskd:5030
SLSKD_USERNAME=your_soulseek_username
SLSKD_PASSWORD=your_soulseek_password

MUSIC_DIR=/mnt/music
PORT=8080

FANART_API_KEY=your_fanart_api_key
```

### Building from source

```bash
git clone https://github.com/ManINeedToSleep/Karasu
cd Karasu
go build -o karasu ./cmd/karasu
./karasu
```

Database migrations run automatically on startup. No manual setup needed.

---

## Result scoring

When Karasu finds multiple Soulseek results for an album, every result is scored and the highest wins. This ensures you always get the best available quality.

| Signal | Points |
|--------|--------|
| Upload speed | `speed / 100,000` |
| Free upload slots | `slots × 10` |
| No free upload slots | `-20` (penalized, not skipped) |
| FLAC files | `+50 per file` |
| 320kbps MP3 | `+20 per file` |
| 256kbps MP3 | `+10 per file` |
| 192kbps MP3 | `+5 per file` |
| 24-bit depth | `+15 per file` |
| Perfect track count match | `+100` |
| Near-perfect match (±1 track) | `+50` |

FLAC will almost always win. An MP3 gets picked only when no FLAC uploaders had free slots or decent speed. Uploaders with no free slots are penalized but not eliminated — they can still win if everyone else has worse quality.

---

## File organization

Downloaded files are automatically moved and renamed:

```
/mnt/music/
└── Kendrick Lamar/
    └── GNX (2024)/
        ├── 01 - wacced out murals.flac
        ├── 02 - squabble up.flac
        ├── 03 - Luther.flac
        └── ...
```

Pattern: `{Artist}/{Album} ({Year})/{Track Number} - {Title}.{ext}`

Every file gets ID3 tags written: title, artist, album, year, genres, track number, disc number.

---

## Release monitoring

Once an artist is marked as monitored, Karasu polls MusicBrainz every 24 hours for new release groups. Any new album, EP, or single not already in your database gets added as `wanted` and automatically queued for download.

The monitor sleeps 2 seconds between each artist check to stay within MusicBrainz rate limits.

---

## Library scanning

Already have music on disk from another tool, ripped CDs, or a previous setup? Hit `POST /api/library/scan`. Karasu walks your music directory and matches files to existing database records using:

1. ID3 tags (most reliable)
2. Filename and folder structure as a fallback

Albums where enough tracks match get marked as `downloaded`.

---

## Part of a larger stack

Karasu is the download and management layer. It pairs with **[Melodix](https://github.com/ManINeedToSleep/Melodix)** — a self-hosted music streaming app for you and your family. Karasu fills the library, Melodix plays it.

```
[Karasu]  ── downloads & organizes ──►  /mnt/music  ◄── streams from ──  [Melodix]
    │                                                                          │
 slskd                                                                    Your family
(Soulseek)
```

---

## Roadmap

- [ ] FLAC tag writing (MP3 only right now)
- [ ] Cover art embedding into audio files
- [ ] Multi-disc album support
- [ ] Lyrics fetching via Genius API
- [ ] Notification webhooks on download completion

---

<div align="center">

*Named for the crow. Built for the hoard.*

</div>
