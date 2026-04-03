-- Karusu initial database migration
-- Creates all core tables for artists, albums, tracks, and genres

-- -----------------------------------------------------------------------------
-- Genres
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS genres (
    id   SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL
);

-- -----------------------------------------------------------------------------
-- Artists
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS artists (
    id               SERIAL PRIMARY KEY,
    name             VARCHAR(255) NOT NULL,
    musicbrainz_id   VARCHAR(36) UNIQUE,           -- MusicBrainz UUID
    bio              TEXT DEFAULT '',
    image_url        TEXT DEFAULT '',
    status           VARCHAR(20) DEFAULT 'continuing',
    monitored        BOOLEAN DEFAULT true,          -- auto-download new releases?
    created_at       TIMESTAMP DEFAULT NOW(),
    updated_at       TIMESTAMP DEFAULT NOW()
);

-- Index for fast name searches
CREATE INDEX IF NOT EXISTS idx_artists_name ON artists(name);
CREATE INDEX IF NOT EXISTS idx_artists_mbid ON artists(musicbrainz_id);

-- -----------------------------------------------------------------------------
-- Artist <-> Genre (many to many)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS artist_genres (
    artist_id INT REFERENCES artists(id) ON DELETE CASCADE,
    genre_id  INT REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (artist_id, genre_id)
);

-- -----------------------------------------------------------------------------
-- Albums
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS albums (
    id               SERIAL PRIMARY KEY,
    artist_id        INT REFERENCES artists(id) ON DELETE CASCADE,
    title            VARCHAR(255) NOT NULL,
    musicbrainz_id   VARCHAR(36) UNIQUE,
    release_date     DATE,
    album_type       VARCHAR(20) DEFAULT 'album',  -- album, ep, single, live, compilation
    cover_url        TEXT DEFAULT '',
    status           VARCHAR(20) DEFAULT 'wanted', -- wanted, downloading, downloaded, missing
    total_tracks     INT DEFAULT 0,
    created_at       TIMESTAMP DEFAULT NOW(),
    updated_at       TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_albums_artist_id ON albums(artist_id);
CREATE INDEX IF NOT EXISTS idx_albums_mbid ON albums(musicbrainz_id);
CREATE INDEX IF NOT EXISTS idx_albums_status ON albums(status);

-- -----------------------------------------------------------------------------
-- Tracks
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tracks (
    id               SERIAL PRIMARY KEY,
    album_id         INT REFERENCES albums(id) ON DELETE CASCADE,
    artist_id        INT REFERENCES artists(id) ON DELETE CASCADE,
    musicbrainz_id   VARCHAR(36),
    title            VARCHAR(255) NOT NULL,
    track_number     INT DEFAULT 0,
    disc_number      INT DEFAULT 1,
    duration_ms      INT DEFAULT 0,               -- duration in milliseconds
    file_path        TEXT DEFAULT '',             -- absolute path on disk e.g. /mnt/music/Artist/Album/01 - Track.flac
    file_format      VARCHAR(10) DEFAULT '',      -- flac, mp3, ogg, opus, etc.
    bitrate          INT DEFAULT 0,               -- in kbps
    status           VARCHAR(20) DEFAULT 'missing',
    -- Lyrics stored so we don't re-fetch every playback
    lyrics_plain     TEXT DEFAULT '',             -- plain text from Genius
    lyrics_lrc       TEXT DEFAULT '',             -- synced .lrc format from LRCLIB
    lyrics_source    VARCHAR(20) DEFAULT '',      -- "genius", "lrclib", or ""
    created_at       TIMESTAMP DEFAULT NOW(),
    updated_at       TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tracks_album_id ON tracks(album_id);
CREATE INDEX IF NOT EXISTS idx_tracks_artist_id ON tracks(artist_id);
CREATE INDEX IF NOT EXISTS idx_tracks_file_path ON tracks(file_path);
CREATE INDEX IF NOT EXISTS idx_tracks_status ON tracks(status);