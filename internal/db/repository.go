package db

import (
	"fmt"
	"karusu/internal/models"
)

// -----------------------------------------------------------------------------
// Artists
// -----------------------------------------------------------------------------

// GetAllArtists returns all artists in the database
func (db *DB) GetAllArtists() ([]models.Artist, error) {
	var artists []models.Artist
	err := db.Select(&artists, `
		SELECT * FROM artists
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("get all artists: %w", err)
	}
	return artists, nil
}

// GetArtistByID returns a single artist by ID
func (db *DB) GetArtistByID(id int) (*models.Artist, error) {
	var artist models.Artist
	err := db.Get(&artist, `
		SELECT * FROM artists WHERE id = $1
	`, id)
	if err != nil {
		return nil, fmt.Errorf("get artist by id: %w", err)
	}
	return &artist, nil
}

// GetArtistByMBID returns a single artist by MusicBrainz ID
func (db *DB) GetArtistByMBID(mbid string) (*models.Artist, error) {
	var artist models.Artist
	err := db.Get(&artist, `
		SELECT * FROM artists WHERE musicbrainz_id = $1
	`, mbid)
	if err != nil {
		return nil, fmt.Errorf("get artist by mbid: %w", err)
	}
	return &artist, nil
}

// CreateArtist inserts a new artist and returns the created record
func (db *DB) CreateArtist(a *models.Artist) error {
	return db.QueryRowx(`
		INSERT INTO artists (name, musicbrainz_id, bio, image_url, status, monitored)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, a.Name, a.MusicBrainzID, a.Bio, a.ImageURL, a.Status, a.Monitored).
		Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

// UpdateArtist updates an existing artist record
func (db *DB) UpdateArtist(a *models.Artist) error {
	_, err := db.Exec(`
		UPDATE artists
		SET name=$1, bio=$2, image_url=$3, status=$4, monitored=$5, updated_at=NOW()
		WHERE id=$6
	`, a.Name, a.Bio, a.ImageURL, a.Status, a.Monitored, a.ID)
	return err
}

// DeleteArtist removes an artist and all their albums/tracks (cascade)
func (db *DB) DeleteArtist(id int) error {
	_, err := db.Exec(`DELETE FROM artists WHERE id = $1`, id)
	return err
}

// -----------------------------------------------------------------------------
// Albums
// -----------------------------------------------------------------------------

// GetAlbumsByArtist returns all albums for a given artist
func (db *DB) GetAlbumsByArtist(artistID int) ([]models.Album, error) {
	var albums []models.Album
	err := db.Select(&albums, `
		SELECT a.*, ar.name as artist_name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		WHERE a.artist_id = $1
		ORDER BY a.release_date DESC
	`, artistID)
	if err != nil {
		return nil, fmt.Errorf("get albums by artist: %w", err)
	}
	return albums, nil
}

// GetAlbumByID returns a single album by ID
func (db *DB) GetAlbumByID(id int) (*models.Album, error) {
	var album models.Album
	err := db.Get(&album, `
		SELECT a.*, ar.name as artist_name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		WHERE a.id = $1
	`, id)
	if err != nil {
		return nil, fmt.Errorf("get album by id: %w", err)
	}
	return &album, nil
}

// CreateAlbum inserts a new album and returns the created record
func (db *DB) CreateAlbum(a *models.Album) error {
	return db.QueryRowx(`
		INSERT INTO albums (artist_id, title, musicbrainz_id, release_date, album_type, cover_url, status, total_tracks)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, a.ArtistID, a.Title, a.MusicBrainzID, a.ReleaseDate, a.AlbumType, a.CoverURL, a.Status, a.TotalTracks).
		Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

// UpdateAlbumStatus updates just the status of an album
func (db *DB) UpdateAlbumStatus(id int, status models.AlbumStatus) error {
	_, err := db.Exec(`
		UPDATE albums SET status=$1, updated_at=NOW() WHERE id=$2
	`, status, id)
	return err
}

// GetAlbumsByStatus returns all albums with a given status
func (db *DB) GetAlbumsByStatus(status models.AlbumStatus) ([]models.Album, error) {
	var albums []models.Album
	err := db.Select(&albums, `
		SELECT a.*, ar.name as artist_name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		WHERE a.status = $1
		ORDER BY a.created_at DESC
	`, status)
	if err != nil {
		return nil, fmt.Errorf("get albums by status: %w", err)
	}
	return albums, nil
}

// -----------------------------------------------------------------------------
// Tracks
// -----------------------------------------------------------------------------

// GetTracksByAlbum returns all tracks for a given album
func (db *DB) GetTracksByAlbum(albumID int) ([]models.Track, error) {
	var tracks []models.Track
	err := db.Select(&tracks, `
		SELECT t.*, ar.name as artist_name, a.title as album_title
		FROM tracks t
		JOIN artists ar ON ar.id = t.artist_id
		JOIN albums a ON a.id = t.album_id
		WHERE t.album_id = $1
		ORDER BY t.disc_number ASC, t.track_number ASC
	`, albumID)
	if err != nil {
		return nil, fmt.Errorf("get tracks by album: %w", err)
	}
	return tracks, nil
}

// CreateTrack inserts a new track
func (db *DB) CreateTrack(t *models.Track) error {
	return db.QueryRowx(`
		INSERT INTO tracks (album_id, artist_id, musicbrainz_id, title, track_number, disc_number, duration_ms, file_path, file_format, bitrate, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at
	`, t.AlbumID, t.ArtistID, t.MusicBrainzID, t.Title, t.TrackNumber, t.DiscNumber, t.DurationMs, t.FilePath, t.FileFormat, t.Bitrate, t.Status).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

// UpdateTrackFilePath updates the file path and status of a track after download
func (db *DB) UpdateTrackFilePath(id int, path, format string, bitrate int) error {
	_, err := db.Exec(`
		UPDATE tracks
		SET file_path=$1, file_format=$2, bitrate=$3, status='downloaded', updated_at=NOW()
		WHERE id=$4
	`, path, format, bitrate, id)
	return err
}

// -----------------------------------------------------------------------------
// Genres
// -----------------------------------------------------------------------------

// GetGenresByArtist returns all genres for a given artist
func (db *DB) GetGenresByArtist(artistID int) ([]models.Genre, error) {
	var genres []models.Genre
	err := db.Select(&genres, `
		SELECT g.* FROM genres g
		JOIN artist_genres ag ON ag.genre_id = g.id
		WHERE ag.artist_id = $1
	`, artistID)
	if err != nil {
		return nil, fmt.Errorf("get genres by artist: %w", err)
	}
	return genres, nil
}

// UpsertGenre inserts a genre if it doesn't exist and returns its ID
func (db *DB) UpsertGenre(name string) (int, error) {
	var id int
	err := db.QueryRowx(`
		INSERT INTO genres (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name
		RETURNING id
	`, name).Scan(&id)
	return id, err
}

// LinkArtistGenre creates a link between an artist and a genre
func (db *DB) LinkArtistGenre(artistID, genreID int) error {
	_, err := db.Exec(`
		INSERT INTO artist_genres (artist_id, genre_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, artistID, genreID)
	return err
}

// FindTrackByTags finds a track by its metadata tags
func (db *DB) FindTrackByTags(title, album, artist string) (*models.Track, error) {
	var track models.Track
	err := db.Get(&track, `
		SELECT t.* FROM tracks t
		JOIN albums a ON a.id = t.album_id
		JOIN artists ar ON ar.id = t.artist_id
		WHERE LOWER(t.title) = LOWER($1)
		AND LOWER(a.title) = LOWER($2)
		AND LOWER(ar.name) = LOWER($3)
		LIMIT 1
	`, title, album, artist)
	if err != nil {
		return nil, err
	}
	return &track, nil
}

// FindTrackByNumber finds a track by artist and track number
func (db *DB) FindTrackByNumber(artistID, trackNumber int) (*models.Track, error) {
	var track models.Track
	err := db.Get(&track, `
		SELECT * FROM tracks
		WHERE artist_id = $1 AND track_number = $2
		LIMIT 1
	`, artistID, trackNumber)
	if err != nil {
		return nil, err
	}
	return &track, nil
}