package library

import (
	"log"
	"time"

	"karasu/internal/db"
	"karasu/internal/metadata"
	"karasu/internal/models"
)

// Monitor checks for new releases from monitored artists and auto-downloads them
type Monitor struct {
	db         *db.DB
	mb         *metadata.MusicBrainzClient
	downloader *Downloader
	interval   time.Duration
}

// NewMonitor creates a new Monitor
func NewMonitor(db *db.DB, mb *metadata.MusicBrainzClient, downloader *Downloader, interval time.Duration) *Monitor {
	return &Monitor{
		db:         db,
		mb:         mb,
		downloader: downloader,
		interval:   interval,
	}
}

// Start begins the monitoring loop in a goroutine
func (m *Monitor) Start() {
	go m.run()
	log.Printf("[Monitor] Started — checking every %s", m.interval)
}

// run is the main monitoring loop
func (m *Monitor) run() {
	// Run immediately on startup, then on the interval
	m.check()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for range ticker.C {
		m.check()
	}
}

// check looks for new releases for all monitored artists
func (m *Monitor) check() {
	log.Println("[Monitor] Checking for new releases...")

	artists, err := m.db.GetAllArtists()
	if err != nil {
		log.Printf("[Monitor] Failed to get artists: %v", err)
		return
	}

	// Only check monitored artists
	for _, artist := range artists {
		if !artist.Monitored {
			continue
		}
		m.checkArtist(&artist)
		// Respect MusicBrainz rate limit between artists
		time.Sleep(2 * time.Second)
	}

	log.Println("[Monitor] Check complete")
}

// checkArtist looks for new releases for a single artist
func (m *Monitor) checkArtist(artist *models.Artist) {
	releaseGroups, err := m.mb.GetArtistReleaseGroups(artist.MusicBrainzID)
	if err != nil {
		log.Printf("[Monitor] Failed to get releases for %s: %v", artist.Name, err)
		return
	}

	// Get existing albums for this artist
	existing, err := m.db.GetAlbumsByArtist(artist.ID)
	if err != nil {
		log.Printf("[Monitor] Failed to get existing albums for %s: %v", artist.Name, err)
		return
	}

	// Build a set of existing MusicBrainz IDs for fast lookup
	existingMBIDs := make(map[string]bool)
	for _, a := range existing {
		existingMBIDs[a.MusicBrainzID] = true
	}

	// Check each release group
	for _, rg := range releaseGroups {
		if rg.PrimaryType == "" {
			continue
		}

		// Skip if we already have this release
		if existingMBIDs[rg.ID] {
			continue
		}

		// New release found!
		log.Printf("[Monitor] New release found for %s: %s", artist.Name, rg.Title)

		releaseDate := ParsePartialDate(rg.FirstRelease)

		album := &models.Album{
			ArtistID:      artist.ID,
			Title:         rg.Title,
			MusicBrainzID: rg.ID,
			ReleaseDate:   releaseDate,
			AlbumType:     NormalizeAlbumType(rg.PrimaryType),
			CoverURL:      metadata.GetCoverArtURL(rg.ID),
			Status:        models.AlbumStatusWanted,
		}

		if err := m.db.CreateAlbum(album); err != nil {
			log.Printf("[Monitor] Failed to save album %s: %v", rg.Title, err)
			continue
		}

		log.Printf("[Monitor] Added %s - %s, starting download...", artist.Name, album.Title)

		// Auto-download immediately
		go func(albumID int) {
			if err := m.downloader.DownloadAlbum(albumID); err != nil {
				log.Printf("[Monitor] Auto-download failed for album %d: %v", albumID, err)
			}
		}(album.ID)
	}
}