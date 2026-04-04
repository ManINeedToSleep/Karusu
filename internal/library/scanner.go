package library

import (
	"fmt"
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

// ProgressFunc is called periodically during a scan.
// found = cumulative files matched so far, msg = human-readable status line.
type ProgressFunc func(found int, msg string)

// Scan walks the music directory and updates the database.
// Kept for backwards compatibility — delegates to ScanWithProgress with no callback.
func (s *Scanner) Scan() (*ScanResult, error) {
	return s.ScanWithProgress(nil)
}

// ScanWithProgress is like Scan but calls progressFn after each file is processed.
// progressFn may be nil — if so, it behaves identically to Scan().
func (s *Scanner) ScanWithProgress(progressFn ProgressFunc) (*ScanResult, error) {
	result := &ScanResult{}

	log.Printf("[Scanner] Starting scan of %s", s.root)

	if progressFn != nil {
		progressFn(0, "Walking music directory…")
	}

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

		linked, err := s.matchFile(path)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return nil
		}
		if linked {
			result.TracksLinked++
		}

		// Report progress after each file
		if progressFn != nil {
			progressFn(result.TracksLinked, fmt.Sprintf("Matched %d / %d files…", result.TracksLinked, result.FilesFound))
		}

		return nil
	})

	if err != nil {
		return result, err
	}

	if progressFn != nil {
		progressFn(result.TracksLinked, "Updating album statuses…")
	}

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
func (s *Scanner) matchFile(path string) (bool, error) {
	if track := s.matchByTags(path); track != nil {
		return s.linkTrack(track, path)
	}
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

	track, err := s.db.FindTrackByTags(title, album, artist)
	if err != nil {
		return nil
	}
	return track
}

// matchByFilename tries to parse track info from the file path
func (s *Scanner) matchByFilename(path string) *models.Track {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil
	}

	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 3 {
		return nil
	}

	artistName := parts[0]
	filename := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))

	trackNum := extractTrackNumber(filename)
	if trackNum == 0 {
		return nil
	}

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

	track, err := s.db.FindTrackByNumber(artistID, trackNum)
	if err != nil {
		return nil
	}
	return track
}

// linkTrack updates a track's file path and status in the database
func (s *Scanner) linkTrack(track *models.Track, path string) (bool, error) {
	if track.FilePath == path {
		return false, nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if err := s.db.UpdateTrackFilePath(track.ID, path, ext, 0); err != nil {
		return false, err
	}
	return true, nil
}

// updateAlbumStatuses marks albums as downloaded when enough tracks are present
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
			downloaded := 0
			for _, t := range tracks {
				if t.FilePath != "" && t.Status == models.TrackStatusDownloaded {
					downloaded++
				}
			}
			if downloaded > 0 && downloaded >= len(tracks)/2 {
				s.db.UpdateAlbumStatus(album.ID, models.AlbumStatusDownloaded)
				marked++
			}
		}
	}
	return marked, nil
}