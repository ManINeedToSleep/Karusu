package library

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"karusu/internal/db"
	"karusu/internal/models"
	"karusu/internal/slskd"
)

const (
	// MaxFilesPerDownload prevents accidentally downloading entire libraries
	// when a wildcard search matches too broadly
	MaxFilesPerDownload = 50
)
	db        *db.DB
	slskd     *slskd.Client
	organizer *Organizer
}

// NewDownloader creates a new Downloader
func NewDownloader(db *db.DB, slskd *slskd.Client, organizer *Organizer) *Downloader {
	return &Downloader{
		db:        db,
		slskd:     slskd,
		organizer: organizer,
	}
}

// -----------------------------------------------------------------------------
// Main pipeline
// -----------------------------------------------------------------------------

// DownloadAlbum runs the full pipeline for a single album:
// search → pick best result → download → organize → update database
func (d *Downloader) DownloadAlbum(albumID int) error {
	// Load album and artist from database
	album, err := d.db.GetAlbumByID(albumID)
	if err != nil {
		return fmt.Errorf("album not found: %w", err)
	}

	artist, err := d.db.GetArtistByID(album.ArtistID)
	if err != nil {
		return fmt.Errorf("artist not found: %w", err)
	}

	tracks, err := d.db.GetTracksByAlbum(albumID)
	if err != nil {
		return fmt.Errorf("failed to get tracks: %w", err)
	}

	log.Printf("[Karusu] Starting download: %s - %s", artist.Name, album.Title)

	// Mark as downloading
	if err := d.db.UpdateAlbumStatus(albumID, models.AlbumStatusDownloading); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Build search query
	query := buildSearchQuery(artist.Name, album.Title)
	log.Printf("[Karusu] Searching for: %s", query)

	// Search slskd
	search, err := d.slskd.StartSearch(query)
	if err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("search failed: %w", err)
	}

	// Wait for results
	if err := d.slskd.WaitForSearch(search.ID, 45*time.Second); err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("search timed out: %w", err)
	}

	results, err := d.slskd.GetSearchResults(search.ID)
	if err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("failed to get results: %w", err)
	}

	if len(results) == 0 {
		// Retry with wildcard — bypasses some Soulseek filters e.g. "*endrick Lamar"
		log.Printf("[Karusu] No results, retrying with wildcard...")
		wildcardQuery := "*" + buildSearchQuery(artist.Name[1:], album.Title)
		search2, err2 := d.slskd.StartSearch(wildcardQuery)
		if err2 == nil {
			d.slskd.WaitForSearch(search2.ID, 45*time.Second)
			results, _ = d.slskd.GetSearchResults(search2.ID)
		}
	}

	if len(results) == 0 {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("no results found for %s", query)
	}

	log.Printf("[Karusu] Found %d results, picking best...", len(results))

	// Pick the best result
	best := pickBestResult(results, album, tracks)
	if best == nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("no suitable result found for %s", query)
	}

	log.Printf("[Karusu] Best result from: %s (%d files, score: %d)",
		best.result.Username, len(best.files), best.score)

	// Safety check — if too many files something went wrong with matching
	if len(best.files) > MaxFilesPerDownload {
		log.Printf("[Karusu] Too many files (%d), truncating to first %d", len(best.files), MaxFilesPerDownload)
		best.files = best.files[:MaxFilesPerDownload]
	}

	// Enqueue all files for download
	for _, f := range best.files {
		if err := d.slskd.EnqueueDownload(best.result.Username, f.Filename, f.Size); err != nil {
			log.Printf("[Karusu] Warning: failed to enqueue %s: %v", f.Filename, err)
		}
	}

	// Wait for all downloads to complete
	log.Printf("[Karusu] Waiting for %d files to download...", len(best.files))
	if err := d.waitForDownloads(best.result.Username, best.files); err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		return fmt.Errorf("downloads failed: %w", err)
	}

	// Organize the downloaded files
	log.Printf("[Karusu] Organizing files...")
	if err := d.organizeDownloads(best.files, tracks, album, artist); err != nil {
		log.Printf("[Karusu] Warning: organize failed: %v", err)
	}

	// Mark album as downloaded
	d.db.UpdateAlbumStatus(albumID, models.AlbumStatusDownloaded)
	log.Printf("[Karusu] ✅ Done: %s - %s", artist.Name, album.Title)

	return nil
}

// DownloadWanted finds all wanted albums and downloads them one by one
func (d *Downloader) DownloadWanted() {
	albums, err := d.db.GetAlbumsByStatus(models.AlbumStatusWanted)
	if err != nil {
		log.Printf("[Karusu] Failed to get wanted albums: %v", err)
		return
	}

	log.Printf("[Karusu] Found %d wanted albums", len(albums))

	for _, album := range albums {
		if err := d.DownloadAlbum(album.ID); err != nil {
			log.Printf("[Karusu] Failed to download album %d: %v", album.ID, err)
		}
		// Be nice to Soulseek — wait a bit between albums
		time.Sleep(5 * time.Second)
	}
}

// -----------------------------------------------------------------------------
// Result scoring
// -----------------------------------------------------------------------------

// scoredResult pairs a search result with its score and matched files
type scoredResult struct {
	result slskd.SearchResult
	files  []slskd.FileResult
	score  int
}

// pickBestResult scores all results and returns the best one
func pickBestResult(results []slskd.SearchResult, album *models.Album, tracks []models.Track) *scoredResult {
	var best *scoredResult

	for _, r := range results {
		// Skip users with no free upload slots
		if r.FreeUploadSlots == 0 {
			continue
		}

		// Filter to only audio files
		audioFiles := filterAudioFiles(r.Files)
		if len(audioFiles) == 0 {
			continue
		}

		score := scoreResult(r, audioFiles, album, tracks)

		if best == nil || score > best.score {
			best = &scoredResult{
				result: r,
				files:  audioFiles,
				score:  score,
			}
		}
	}

	return best
}

// scoreResult calculates a quality score for a search result
// Higher is better
func scoreResult(r slskd.SearchResult, files []slskd.FileResult, album *models.Album, tracks []models.Track) int {
	score := 0

	// Upload speed bonus (faster = better)
	score += r.UploadSpeed / 100000 // normalize to reasonable range

	// Free slots bonus
	score += r.FreeUploadSlots * 10

	// Check file formats — FLAC is king
	hasFlac := false
	hasMp3 := false
	totalBitrate := 0

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Filename))
		switch ext {
		case ".flac":
			hasFlac = true
			score += 50 // big bonus for FLAC
		case ".mp3":
			hasMp3 = true
		}

		// Bitrate bonus
		if f.BitRate >= 320 {
			score += 20
		} else if f.BitRate >= 256 {
			score += 10
		} else if f.BitRate >= 192 {
			score += 5
		}
		totalBitrate += f.BitRate

		// Bit depth bonus (for FLAC — 24bit > 16bit)
		if f.BitDepth >= 24 {
			score += 15
		}
	}

	_ = hasMp3
	_ = hasFlac
	_ = totalBitrate

	// Completeness bonus — does the number of files match the expected track count?
	if album.TotalTracks > 0 {
		if len(files) == album.TotalTracks {
			score += 100 // perfect match
		} else if len(files) >= album.TotalTracks-1 {
			score += 50 // close enough
		}
	} else if len(tracks) > 0 {
		// Fall back to our db track count
		if len(files) == len(tracks) {
			score += 100
		}
	}

	return score
}

// filterAudioFiles returns only audio files from a list of files
func filterAudioFiles(files []slskd.FileResult) []slskd.FileResult {
	var audio []slskd.FileResult
	for _, f := range files {
		if isAudioFile(f.Filename) {
			audio = append(audio, f)
		}
	}
	return audio
}

// -----------------------------------------------------------------------------
// Download waiting
// -----------------------------------------------------------------------------

// waitForDownloads polls until all files from a user are done downloading
func (d *Downloader) waitForDownloads(username string, files []slskd.FileResult) error {
	// Build a set of filenames we're waiting for
	waiting := make(map[string]bool)
	for _, f := range files {
		waiting[f.Filename] = true
	}

	deadline := time.Now().Add(30 * time.Minute)

	for time.Now().Before(deadline) {
		transfers, err := d.slskd.GetAllDownloads()
		if err != nil {
			return fmt.Errorf("failed to get downloads: %w", err)
		}

		completed := 0
		failed := 0

		for _, t := range transfers {
			if !waiting[t.Filename] {
				continue
			}
			switch t.State {
			case "Completed, Succeeded":
				completed++
			case "Completed, Errored", "Completed, Cancelled":
				failed++
				log.Printf("[Karusu] Download failed: %s (%s)", t.Filename, t.State)
			}
		}

		log.Printf("[Karusu] Progress: %d/%d completed, %d failed",
			completed, len(files), failed)

		// All done if completed + failed = total
		if completed+failed >= len(files) {
			if failed > 0 {
				log.Printf("[Karusu] Warning: %d files failed to download", failed)
			}
			return nil
		}

		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("downloads timed out after 30 minutes")
}

// -----------------------------------------------------------------------------
// File organization
// -----------------------------------------------------------------------------

// organizeDownloads moves downloaded files into the music library
func (d *Downloader) organizeDownloads(files []slskd.FileResult, tracks []models.Track, album *models.Album, artist *models.Artist) error {
	// Build a map of track number → track for matching
	trackMap := make(map[int]*models.Track)
	for i := range tracks {
		trackMap[tracks[i].TrackNumber] = &tracks[i]
	}

	for _, f := range files {
		// The downloaded file lives in the slskd downloads folder
		// slskd saves files as: /app/downloads/{username}/{filename}
		downloadPath := filepath.Join("/mnt/music/downloads", filepath.Base(f.Filename))

		// Try to match this file to a track by track number
		trackNum := extractTrackNumber(f.Filename)
		track, ok := trackMap[trackNum]
		if !ok {
			log.Printf("[Karusu] Warning: couldn't match file to track: %s", f.Filename)
			continue
		}

		// Move and rename the file
		newPath, err := d.organizer.OrganizeTrack(downloadPath, track, album, artist)
		if err != nil {
			log.Printf("[Karusu] Warning: failed to organize %s: %v", f.Filename, err)
			continue
		}

		// Update the track in the database
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(newPath)), ".")
		if err := d.db.UpdateTrackFilePath(track.ID, newPath, ext, f.BitRate); err != nil {
			log.Printf("[Karusu] Warning: failed to update track path: %v", err)
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// buildSearchQuery creates a Soulseek search string for an album
func buildSearchQuery(artistName, albumTitle string) string {
	return fmt.Sprintf("%s %s", artistName, albumTitle)
}

// extractTrackNumber tries to pull a track number from a filename
// e.g. "01 - Alright.flac" → 1
//      "03. DNA.mp3" → 3
func extractTrackNumber(filename string) int {
	base := filepath.Base(filename)
	var num int
	// Try common patterns: "01 -", "01.", "01 "
	fmt.Sscanf(base, "%d", &num)
	return num
}