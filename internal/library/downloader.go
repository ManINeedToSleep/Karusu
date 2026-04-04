package library

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"karasu/internal/api"
	"karasu/internal/db"
	"karasu/internal/models"
	"karasu/internal/slskd"
)

const (
	MaxFilesPerDownload = 50
)

// Downloader manages the full pipeline of searching, downloading, and organizing music
type Downloader struct {
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

func (d *Downloader) DownloadAlbum(albumID int) error {
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

	log.Printf("[Karasu] Starting download: %s - %s", artist.Name, album.Title)

	if err := d.db.UpdateAlbumStatus(albumID, models.AlbumStatusDownloading); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Seed progress at 0% so the UI shows something immediately
	api.SetAlbumProgress(albumID, 0, "", "")

	query := buildSearchQuery(artist.Name, album.Title)
	log.Printf("[Karasu] Searching for: %s", query)

	search, err := d.slskd.StartSearch(query)
	if err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("search failed: %w", err)
	}

	if err := d.slskd.WaitForSearch(search.ID, 45*time.Second); err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("search timed out: %w", err)
	}

	results, err := d.slskd.GetSearchResults(search.ID)
	if err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("failed to get results: %w", err)
	}

	if len(results) == 0 {
		log.Printf("[Karasu] No results, retrying with wildcard...")
		wildcardQuery := "*" + buildSearchQuery(artist.Name[1:], album.Title)
		search2, err2 := d.slskd.StartSearch(wildcardQuery)
		if err2 == nil {
			d.slskd.WaitForSearch(search2.ID, 45*time.Second)
			results, _ = d.slskd.GetSearchResults(search2.ID)
		}
	}

	if len(results) == 0 {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("no results found for %s", query)
	}

	log.Printf("[Karasu] Found %d results, picking best...", len(results))

	var best *scoredResult
	tried := 0
	for tried < 5 {
		best = pickNthBestResult(results, album, tracks, tried)
		if best == nil {
			break
		}

		if len(best.files) > MaxFilesPerDownload {
			log.Printf("[Karasu] Too many files (%d), truncating to first %d", len(best.files), MaxFilesPerDownload)
			best.files = best.files[:MaxFilesPerDownload]
		}

		log.Printf("[Karasu] Trying result %d: %s (%d files, score: %d)",
			tried+1, best.result.Username, len(best.files), best.score)

		failed := 0
		for _, f := range best.files {
			if err := d.slskd.EnqueueDownload(best.result.Username, f.Filename, f.Size); err != nil {
				failed++
			}
		}

		if failed < len(best.files) {
			break
		}

		log.Printf("[Karasu] All enqueues failed for %s, trying next result...", best.result.Username)
		tried++
	}

	if best == nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("no suitable result found for %s", query)
	}

	log.Printf("[Karasu] Waiting for %d files to download...", len(best.files))
	if err := d.waitForDownloads(albumID, best.result.Username, best.files); err != nil {
		d.db.UpdateAlbumStatus(albumID, models.AlbumStatusMissing)
		api.ClearAlbumProgress(albumID)
		return fmt.Errorf("downloads failed: %w", err)
	}

	log.Printf("[Karasu] Organizing files...")
	if err := d.organizeDownloads(best.files, tracks, album, artist); err != nil {
		log.Printf("[Karasu] Warning: organize failed: %v", err)
	}

	d.db.UpdateAlbumStatus(albumID, models.AlbumStatusDownloaded)
	api.ClearAlbumProgress(albumID) // clean up — album is done
	log.Printf("[Karasu] ✅ Done: %s - %s", artist.Name, album.Title)

	return nil
}

func (d *Downloader) DownloadWanted() {
	albums, err := d.db.GetAlbumsByStatus(models.AlbumStatusWanted)
	if err != nil {
		log.Printf("[Karasu] Failed to get wanted albums: %v", err)
		return
	}

	log.Printf("[Karasu] Found %d wanted albums", len(albums))

	for _, album := range albums {
		if err := d.DownloadAlbum(album.ID); err != nil {
			log.Printf("[Karasu] Failed to download album %d: %v", album.ID, err)
		}
		time.Sleep(5 * time.Second)
	}
}

// -----------------------------------------------------------------------------
// Result scoring
// -----------------------------------------------------------------------------

type scoredResult struct {
	result slskd.SearchResult
	files  []slskd.FileResult
	score  int
}

func pickBestResult(results []slskd.SearchResult, album *models.Album, tracks []models.Track) *scoredResult {
	var best *scoredResult

	for _, r := range results {
		audioFiles := filterAudioFiles(r.Files)
		if len(audioFiles) == 0 {
			continue
		}
		folders := groupByFolder(audioFiles)
		for _, folderFiles := range folders {
			score := scoreResult(r, folderFiles, album, tracks)
			log.Printf("[Karasu] Result from %-20s | Files: %d | Slots: %d | Speed: %d",
				r.Username, len(folderFiles), r.FreeUploadSlots, r.UploadSpeed)
			if best == nil || score > best.score {
				best = &scoredResult{result: r, files: folderFiles, score: score}
			}
		}
	}

	return best
}

func pickNthBestResult(results []slskd.SearchResult, album *models.Album, tracks []models.Track, n int) *scoredResult {
	var scored []*scoredResult

	for _, r := range results {
		audioFiles := filterAudioFiles(r.Files)
		if len(audioFiles) == 0 {
			continue
		}
		for _, folderFiles := range groupByFolder(audioFiles) {
			score := scoreResult(r, folderFiles, album, tracks)
			scored = append(scored, &scoredResult{result: r, files: folderFiles, score: score})
		}
	}

	for i := 0; i <= n; i++ {
		if i >= len(scored) {
			return nil
		}
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	if n >= len(scored) {
		return nil
	}
	return scored[n]
}

func groupByFolder(files []slskd.FileResult) map[string][]slskd.FileResult {
	groups := make(map[string][]slskd.FileResult)
	for _, f := range files {
		dir := f.Filename
		if idx := strings.LastIndexAny(f.Filename, "/\\"); idx >= 0 {
			dir = f.Filename[:idx]
		}
		groups[dir] = append(groups[dir], f)
	}
	return groups
}

func scoreResult(r slskd.SearchResult, files []slskd.FileResult, album *models.Album, tracks []models.Track) int {
	score := 0
	score += r.UploadSpeed / 100000
	score += r.FreeUploadSlots * 10

	hasFlac := false
	hasMp3 := false
	totalBitrate := 0

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Filename))
		switch ext {
		case ".flac":
			hasFlac = true
			score += 50
		case ".mp3":
			hasMp3 = true
		}
		if f.BitRate >= 320 {
			score += 20
		} else if f.BitRate >= 256 {
			score += 10
		} else if f.BitRate >= 192 {
			score += 5
		}
		totalBitrate += f.BitRate
		if f.BitDepth >= 24 {
			score += 15
		}
	}

	_ = hasMp3
	_ = hasFlac
	_ = totalBitrate

	if album.TotalTracks > 0 {
		if len(files) == album.TotalTracks {
			score += 100
		} else if len(files) >= album.TotalTracks-1 {
			score += 50
		}
	} else if len(tracks) > 0 {
		if len(files) == len(tracks) {
			score += 100
		}
	}

	return score
}

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
// Download waiting — with live progress reporting
// -----------------------------------------------------------------------------

// waitForDownloads polls until all files are done and reports progress
// to the API layer via api.SetAlbumProgress so the UI can display it.
func (d *Downloader) waitForDownloads(albumID int, username string, files []slskd.FileResult) error {
	waiting := make(map[string]bool)
	for _, f := range files {
		waiting[f.Filename] = true
	}

	total := len(files)
	deadline := time.Now().Add(5 * time.Minute)
	lastCompleted := 0
	staleSince := time.Now()

	for time.Now().Before(deadline) {
		transfers, err := d.slskd.GetAllDownloads()
		if err != nil {
			return fmt.Errorf("failed to get downloads: %w", err)
		}

		completed := 0
		failed := 0
		queued := 0
		var totalSpeed int64  // bytes/sec across active transfers
		activeCount := 0

		for _, t := range transfers {
			if !waiting[t.Filename] {
				continue
			}
			switch t.State {
			case "Completed, Succeeded":
				completed++
			case "Completed, Errored", "Completed, Cancelled":
				failed++
				log.Printf("[Karasu] Download failed: %s (%s)", t.Filename, t.State)
			case "Queued, Remotely", "Queued":
				queued++
			default:
				// Actively transferring — accumulate bytes transferred
				if t.BytesTransferred > 0 {
					totalSpeed += t.BytesTransferred
					activeCount++
				}
			}
		}

		// Calculate percent and format speed string
		percent := 0.0
		if total > 0 {
			percent = float64(completed) / float64(total) * 100
		}

		speedStr := ""
		if totalSpeed > 0 {
			mbps := float64(totalSpeed) / 1_000_000
			speedStr = fmt.Sprintf("%.1f MB/s", mbps)
		}

		// Push live progress to the API layer
		api.SetAlbumProgress(albumID, percent, speedStr, "")

		log.Printf("[Karasu] Progress: %d/%d completed, %d failed, %d queued — %.0f%%",
			completed, total, failed, queued, percent)

		if completed > lastCompleted {
			lastCompleted = completed
			staleSince = time.Now()
		}

		if queued > 0 && completed == 0 && time.Since(staleSince) > 2*time.Minute {
			return fmt.Errorf("downloads stuck in remote queue, trying next user")
		}

		if completed+failed >= total {
			if failed > 0 {
				log.Printf("[Karasu] Warning: %d files failed to download", failed)
			}
			return nil
		}

		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("downloads timed out")
}

// -----------------------------------------------------------------------------
// File organization
// -----------------------------------------------------------------------------

func (d *Downloader) organizeDownloads(files []slskd.FileResult, tracks []models.Track, album *models.Album, artist *models.Artist) error {
	trackMap := make(map[int]*models.Track)
	for i := range tracks {
		trackMap[tracks[i].TrackNumber] = &tracks[i]
	}

	for _, f := range files {
		downloadPath := filepath.Join("/mnt/music/downloads", filepath.Base(f.Filename))
		trackNum := extractTrackNumber(f.Filename)
		track, ok := trackMap[trackNum]
		if !ok {
			log.Printf("[Karasu] Warning: couldn't match file to track: %s", f.Filename)
			continue
		}
		newPath, err := d.organizer.OrganizeTrack(downloadPath, track, album, artist)
		if err != nil {
			log.Printf("[Karasu] Warning: failed to organize %s: %v", f.Filename, err)
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(newPath)), ".")
		if err := d.db.UpdateTrackFilePath(track.ID, newPath, ext, f.BitRate); err != nil {
			log.Printf("[Karasu] Warning: failed to update track path: %v", err)
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func buildSearchQuery(artistName, albumTitle string) string {
	return fmt.Sprintf("%s %s", artistName, albumTitle)
}

func extractTrackNumber(filename string) int {
	base := filepath.Base(filename)
	var num int
	fmt.Sscanf(base, "%d", &num)
	return num
}