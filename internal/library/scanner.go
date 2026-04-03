package library

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"karasu/internal/db"
	"karasu/internal/models"

	"github.com/dhowden/tag"
)

// Scanner walks the music directory and reconciles files with the database
type Scanner struct {
	db   *db.DB
	root string
}

// NewScanner creates a new Scanner
func NewScanner(db *db.DB, root string) *Scanner {
	return &Scanner{db: db, root: root}
}

// ScanResult holds the results of a scan
type ScanResult struct {
	FilesFound   int
	TracksLinked int
	AlbumsMarked int
	Errors       []string
}

// Scan walks the music directory and updates the database
func (s *Scanner) Scan() (*ScanResult, error) {
	result := &ScanResult{}

	log.Printf("[Scanner] Starting scan of %s", s.root)

	// Walk every audio file in the music directory
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors, keep walking
		}

		// Skip the downloads folder — those aren't organized yet
		if info.IsDir() && info.Name() == "downloads" {
			return filepath.SkipDir
		}

		if info.IsDir() || !isAudioFile(path) {
			return nil
		}

		result.FilesFound++

		// Try to match this file to a track in the database
		linked, err := s.matchFile(path)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return nil
		}
		if linked {
			result.TracksLinked++
		}

		return nil
	})

	if err != nil {
		return result, err
	}

	// After linking tracks, update album statuses
	marked, err := s.updateAlbumStatuses()
	if err != nil {
		log.Printf("[Scanner] Failed to update album statuses: %v", err)
	}
	result.AlbumsMarked = marked

	log.Printf("[Scanner] Done — %d files found, %d tracks linked, %d albums marked downloaded",
		result.FilesFound, result.TracksLinked, result.AlbumsMarked)

	return result, nil
}

// matchFile tries to match an audio file to a track in the database
// It tries two strategies: reading ID3 tags, then falling back to filename parsing
func (s *Scanner) matchFile(path string) (bool, error) {
	// Strategy 1: read tags from the file
	if track := s.matchByTags(path); track != nil {
		return s.linkTrack(track, path)
	}

	// Strategy 2: parse the filename
	if track := s.matchByFilename(path); track != nil {
		return s.linkTrack(track, path)
	}

	return false, nil
}

// matchByTags reads ID3/Vorbis tags and tries to find a matching track
func (s *Scanner) matchByTags(path string) *models.Track {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil
	}

	title := m.Title()
	album := m.Album()
	artist := m.Artist()

	if title == "" || album == "" {
		return nil
	}

	// Find the track in the database by title + album + artist
	track, err := s.db.FindTrackByTags(title, album, artist)
	if err != nil {
		return nil
	}

	return track
}

// matchByFilename tries to parse track info from the file path
// e.g. /mnt/music/Ado/狂言 (2022)/01 - レディメイド.flac
func (s *Scanner) matchByFilename(path string) *models.Track {
	// Get relative path from music root
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil
	}

	// Split into parts: Artist / Album / Filename
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 3 {
		return nil
	}

	artistName := parts[0]
	// albumName := parts[1] // available if needed
	filename := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))

	// Extract track number from filename e.g. "01 - レディメイド" → 1
	trackNum := extractTrackNumber(filename)
	if trackNum == 0 {
		return nil
	}

	// Find artist in database
	artists, err := s.db.GetAllArtists()
	if err != nil {
		return nil
	}

	var artistID int
	for _, a := range artists {
		if strings.EqualFold(a.Name, artistName) {
			artistID = a.ID
			break
		}
	}

	if artistID == 0 {
		return nil
	}

	// Find track by artist + track number
	track, err := s.db.FindTrackByNumber(artistID, trackNum)
	if err != nil {
		return nil
	}

	return track
}

// linkTrack updates a track's file path and status in the database
func (s *Scanner) linkTrack(track *models.Track, path string) (bool, error) {
	// Already linked to this path
	if track.FilePath == path {
		return false, nil
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if err := s.db.UpdateTrackFilePath(track.ID, path, ext, 0); err != nil {
		return false, err
	}

	return true, nil
}

// updateAlbumStatuses checks each album and marks it downloaded if all tracks are present
func (s *Scanner) updateAlbumStatuses() (int, error) {
	artists, err := s.db.GetAllArtists()
	if err != nil {
		return 0, err
	}

	marked := 0
	for _, artist := range artists {
		albums, err := s.db.GetAlbumsByArtist(artist.ID)
		if err != nil {
			continue
		}

		for _, album := range albums {
			if album.Status == models.AlbumStatusDownloaded {
				continue
			}

			tracks, err := s.db.GetTracksByAlbum(album.ID)
			if err != nil || len(tracks) == 0 {
				continue
			}

			// Count how many tracks have file paths
			downloaded := 0
			for _, t := range tracks {
				if t.FilePath != "" && t.Status == models.TrackStatusDownloaded {
					downloaded++
				}
			}

			// If more than half the tracks are downloaded, mark album as downloaded
			if downloaded > 0 && downloaded >= len(tracks)/2 {
				s.db.UpdateAlbumStatus(album.ID, models.AlbumStatusDownloaded)
				marked++
			}
		}
	}

	return marked, nil
}