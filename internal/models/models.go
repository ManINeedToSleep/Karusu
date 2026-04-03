package models

import "time"

// DSN builds a PostgreSQL connection string from individual parts
// e.g. DSN("localhost", "5432", "karusu", "neosgw", "password")

// -----------------------------------------------------------------------------
// Enums
// -----------------------------------------------------------------------------

type ArtistStatus string
const (
	ArtistStatusContinuing ArtistStatus = "continuing"
	ArtistStatusEnded      ArtistStatus = "ended"
)

type AlbumType string
const (
	AlbumTypeAlbum       AlbumType = "album"
	AlbumTypeEP          AlbumType = "ep"
	AlbumTypeSingle      AlbumType = "single"
	AlbumTypeLive        AlbumType = "live"
	AlbumTypeCompilation AlbumType = "compilation"
)

type AlbumStatus string
const (
	AlbumStatusWanted      AlbumStatus = "wanted"      // we want this but don't have it
	AlbumStatusDownloading AlbumStatus = "downloading" // slskd is grabbing it right now
	AlbumStatusDownloaded  AlbumStatus = "downloaded"  // we have it
	AlbumStatusMissing     AlbumStatus = "missing"     // couldn't find it on Soulseek
)

type TrackStatus string
const (
	TrackStatusDownloaded TrackStatus = "downloaded"
	TrackStatusMissing    TrackStatus = "missing"
)

// -----------------------------------------------------------------------------
// Artist
// -----------------------------------------------------------------------------

type Artist struct {
	ID              int          `db:"id" json:"id"`
	Name            string       `db:"name" json:"name"`
	MusicBrainzID   string       `db:"musicbrainz_id" json:"musicbrainzId"`
	Bio             string       `db:"bio" json:"bio"`
	ImageURL        string       `db:"image_url" json:"imageUrl"`
	Status          ArtistStatus `db:"status" json:"status"`
	Monitored       bool         `db:"monitored" json:"monitored"`   // if true, auto-download new releases
	Genres          []string     `db:"-" json:"genres"`              // fetched separately
	CreatedAt       time.Time    `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time    `db:"updated_at" json:"updatedAt"`
}

// -----------------------------------------------------------------------------
// Album
// -----------------------------------------------------------------------------

type Album struct {
	ID              int         `db:"id" json:"id"`
	ArtistID        int         `db:"artist_id" json:"artistId"`
	Title           string      `db:"title" json:"title"`
	MusicBrainzID   string      `db:"musicbrainz_id" json:"musicbrainzId"`
	ReleaseDate     time.Time   `db:"release_date" json:"releaseDate"`
	AlbumType       AlbumType   `db:"album_type" json:"albumType"`
	CoverURL        string      `db:"cover_url" json:"coverUrl"`
	Status          AlbumStatus `db:"status" json:"status"`
	TotalTracks     int         `db:"total_tracks" json:"totalTracks"`
	CreatedAt       time.Time   `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time   `db:"updated_at" json:"updatedAt"`

	// Joined fields — not stored in albums table, populated by queries
	ArtistName      string      `db:"artist_name" json:"artistName,omitempty"`
	Tracks          []Track     `db:"-" json:"tracks,omitempty"`
}

// -----------------------------------------------------------------------------
// Track
// -----------------------------------------------------------------------------

type Track struct {
	ID              int         `db:"id" json:"id"`
	AlbumID         int         `db:"album_id" json:"albumId"`
	ArtistID        int         `db:"artist_id" json:"artistId"`
	MusicBrainzID   string      `db:"musicbrainz_id" json:"musicbrainzId"`
	Title           string      `db:"title" json:"title"`
	TrackNumber     int         `db:"track_number" json:"trackNumber"`
	DiscNumber      int         `db:"disc_number" json:"discNumber"`
	DurationMs      int         `db:"duration_ms" json:"durationMs"`   // duration in milliseconds
	FilePath        string      `db:"file_path" json:"filePath"`       // absolute path on disk
	FileFormat      string      `db:"file_format" json:"fileFormat"`   // flac, mp3, ogg, etc.
	Bitrate         int         `db:"bitrate" json:"bitrate"`          // in kbps
	Status          TrackStatus `db:"status" json:"status"`
	// Lyrics — populated by Melodix later, stored so we don't re-fetch every time
	LyricsPlain     string      `db:"lyrics_plain" json:"lyricsPlain"`         // plain text from Genius
	LyricsLRC       string      `db:"lyrics_lrc" json:"lyricsLrc"`             // synced .lrc format from LRCLIB
	LyricsSource    string      `db:"lyrics_source" json:"lyricsSource"`       // "genius", "lrclib", or ""
	CreatedAt       time.Time   `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time   `db:"updated_at" json:"updatedAt"`

	// Joined fields
	ArtistName      string      `db:"artist_name" json:"artistName,omitempty"`
	AlbumTitle      string      `db:"album_title" json:"albumTitle,omitempty"`
}

// -----------------------------------------------------------------------------
// Genre (many-to-many with artists)
// -----------------------------------------------------------------------------

type Genre struct {
	ID   int    `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
}

// ArtistGenre is the join table between artists and genres
type ArtistGenre struct {
	ArtistID int `db:"artist_id"`
	GenreID  int `db:"genre_id"`
}