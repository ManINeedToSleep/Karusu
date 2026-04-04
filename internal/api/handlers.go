package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"karasu/internal/db"
	"karasu/internal/library"
	"karasu/internal/metadata"
	"karasu/internal/models"
)

// Handler holds all dependencies needed by the API handlers
type Handler struct {
	db         *db.DB
	mb         *metadata.MusicBrainzClient
	fanart     *metadata.FanartClient
	downloader *library.Downloader
}

// NewHandler creates a new Handler
func NewHandler(db *db.DB, mb *metadata.MusicBrainzClient, fanart *metadata.FanartClient, downloader *library.Downloader) *Handler {
	return &Handler{db: db, mb: mb, fanart: fanart, downloader: downloader}
}

// RegisterRoutes wires up all API routes to the gin router
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.GET("/search", h.searchArtists)

		api.GET("/artists", h.listArtists)
		api.POST("/artists", h.addArtist)
		api.GET("/artists/:id", h.getArtist)
		api.DELETE("/artists/:id", h.deleteArtist)
		api.PUT("/artists/:id/monitored", h.toggleMonitored)

		api.GET("/albums/:id", h.getAlbum)
		api.PUT("/albums/:id/download", h.downloadAlbum)
		api.GET("/albums/:id/progress", h.getAlbumProgress) // NEW

		api.GET("/library/wanted", h.getWanted)
		api.POST("/library/scan", h.scanLibrary)
		api.GET("/library/scan/status", h.getScanStatus) // NEW
	}
}

// ── Scan state ────────────────────────────────────────────────
// One scan at a time. Lives in memory — resets on restart.

type scanPhase string

const (
	scanIdle      scanPhase = "idle"
	scanScanning  scanPhase = "scanning"
	scanComplete  scanPhase = "complete"
	scanError     scanPhase = "error"
)

var scanState = struct {
	mu         sync.Mutex
	phase      scanPhase
	filesFound int
	message    string
}{phase: scanIdle}

// ── Album progress store ──────────────────────────────────────
// Populated by the downloader goroutine, read by getAlbumProgress.

type albumProgress struct {
	AlbumID int     `json:"albumId"`
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
}

var progressStore = struct {
	mu   sync.Mutex
	data map[int]*albumProgress
}{data: make(map[int]*albumProgress)}

// SetAlbumProgress is called from library/downloader.go during a download.
func SetAlbumProgress(albumID int, percent float64, speed, eta string) {
	progressStore.mu.Lock()
	progressStore.data[albumID] = &albumProgress{
		AlbumID: albumID,
		Percent: percent,
		Speed:   speed,
		ETA:     eta,
	}
	progressStore.mu.Unlock()
}

// ClearAlbumProgress removes a finished album from the progress store.
func ClearAlbumProgress(albumID int) {
	progressStore.mu.Lock()
	delete(progressStore.data, albumID)
	progressStore.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Search
// -----------------------------------------------------------------------------

func (h *Handler) searchArtists(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}
	artists, err := h.mb.SearchArtists(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, artists)
}

// -----------------------------------------------------------------------------
// Artists
// -----------------------------------------------------------------------------

func (h *Handler) listArtists(c *gin.Context) {
	artists, err := h.db.GetAllArtists()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range artists {
		genres, err := h.db.GetGenresByArtist(artists[i].ID)
		if err == nil {
			for _, g := range genres {
				artists[i].Genres = append(artists[i].Genres, g.Name)
			}
		}
	}
	c.JSON(http.StatusOK, artists)
}

func (h *Handler) addArtist(c *gin.Context) {
	var req struct {
		MusicBrainzID string `json:"musicbrainzId" binding:"required"`
		Monitored     bool   `json:"monitored"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, _ := h.db.GetArtistByMBID(req.MusicBrainzID)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "artist already exists"})
		return
	}

	mbArtist, err := h.mb.GetArtist(req.MusicBrainzID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch artist from MusicBrainz"})
		return
	}

	artist := &models.Artist{
		Name:          mbArtist.Name,
		MusicBrainzID: mbArtist.ID,
		Status:        models.ArtistStatusContinuing,
		Monitored:     req.Monitored,
	}
	if err := h.db.CreateArtist(artist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save artist"})
		return
	}

	go func() {
		imageURL, err := h.fanart.GetArtistImageURL(artist.MusicBrainzID)
		if err == nil && imageURL != "" {
			artist.ImageURL = imageURL
			h.db.UpdateArtist(artist)
		}
	}()
	go h.syncDiscography(artist)

	c.JSON(http.StatusCreated, artist)
}

func (h *Handler) getArtist(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	artist, err := h.db.GetArtistByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artist not found"})
		return
	}
	albums, err := h.db.GetAlbumsByArtist(id)
	if err == nil {
		for i := range albums {
			tracks, _ := h.db.GetTracksByAlbum(albums[i].ID)
			albums[i].Tracks = tracks
		}
	}
	c.JSON(http.StatusOK, gin.H{"artist": artist, "albums": albums})
}

func (h *Handler) deleteArtist(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.db.DeleteArtist(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "artist deleted"})
}

func (h *Handler) toggleMonitored(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		Monitored bool `json:"monitored"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	artist, err := h.db.GetArtistByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artist not found"})
		return
	}
	artist.Monitored = req.Monitored
	if err := h.db.UpdateArtist(artist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, artist)
}

// -----------------------------------------------------------------------------
// Albums
// -----------------------------------------------------------------------------

func (h *Handler) getAlbum(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	album, err := h.db.GetAlbumByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "album not found"})
		return
	}
	tracks, _ := h.db.GetTracksByAlbum(id)
	album.Tracks = tracks
	c.JSON(http.StatusOK, album)
}

func (h *Handler) downloadAlbum(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := h.db.GetAlbumByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "album not found"})
		return
	}
	if err := h.db.UpdateAlbumStatus(id, models.AlbumStatusDownloading); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	go func() {
		if err := h.downloader.DownloadAlbum(id); err != nil {
			log.Printf("[Karasu] Download failed for album %d: %v", id, err)
			ClearAlbumProgress(id) // clean up on failure too
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "download started", "albumId": id})
}

// getAlbumProgress returns live download progress for an album.
// GET /api/albums/:id/progress
func (h *Handler) getAlbumProgress(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	// Check in-memory progress first (populated by downloader goroutine)
	progressStore.mu.Lock()
	p, ok := progressStore.data[id]
	progressStore.mu.Unlock()

	if ok {
		c.JSON(http.StatusOK, p)
		return
	}

	// Fallback: derive from DB status so the UI always gets a valid response
	album, err := h.db.GetAlbumByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "album not found"})
		return
	}

	pct := 0.0
	if album.Status == models.AlbumStatusDownloaded {
		pct = 100.0
	}

	c.JSON(http.StatusOK, albumProgress{AlbumID: id, Percent: pct})
}

// -----------------------------------------------------------------------------
// Library
// -----------------------------------------------------------------------------

func (h *Handler) getWanted(c *gin.Context) {
	albums, err := h.db.GetAlbumsByStatus(models.AlbumStatusWanted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, albums)
}

// scanLibrary kicks off a background scan and returns immediately.
// The UI polls GET /api/library/scan/status for live progress.
// POST /api/library/scan
func (h *Handler) scanLibrary(c *gin.Context) {
	scanState.mu.Lock()
	if scanState.phase == scanScanning {
		scanState.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "scan already in progress"})
		return
	}
	scanState.phase = scanScanning
	scanState.filesFound = 0
	scanState.message = "Starting scan…"
	scanState.mu.Unlock()

	go func() {
		scanner := library.NewScanner(h.db, os.Getenv("MUSIC_DIR"))
		result, err := scanner.ScanWithProgress(func(found int, msg string) {
			scanState.mu.Lock()
			scanState.filesFound = found
			scanState.message = msg
			scanState.mu.Unlock()
		})

		scanState.mu.Lock()
		if err != nil {
			scanState.phase = scanError
			scanState.message = err.Error()
			scanState.filesFound = 0
		} else {
			scanState.phase = scanComplete
			scanState.filesFound = result.FilesFound
			scanState.message = ""
		}
		scanState.mu.Unlock()
	}()

	c.JSON(http.StatusAccepted, gin.H{"status": "scan started"})
}

// getScanStatus returns the current state of the library scan.
// GET /api/library/scan/status
func (h *Handler) getScanStatus(c *gin.Context) {
	scanState.mu.Lock()
	defer scanState.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"phase":      scanState.phase,
		"filesFound": scanState.filesFound,
		"message":    scanState.message,
	})
}

// -----------------------------------------------------------------------------
// Background jobs
// -----------------------------------------------------------------------------

func (h *Handler) syncDiscography(artist *models.Artist) {
	releaseGroups, err := h.mb.GetArtistReleaseGroups(artist.MusicBrainzID)
	if err != nil {
		return
	}
	for _, rg := range releaseGroups {
		if rg.PrimaryType == "" {
			continue
		}
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
		existing, _ := h.db.GetAlbumByID(0)
		_ = existing
		if err := h.db.CreateAlbum(album); err != nil {
			continue
		}
	}
}