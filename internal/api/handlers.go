package api

import (
	"log"
	"net/http"
	"os"
	"strconv"

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
		// Search MusicBrainz for artists to add
		api.GET("/search", h.searchArtists)

		// Artist CRUD
		api.GET("/artists", h.listArtists)
		api.POST("/artists", h.addArtist)
		api.GET("/artists/:id", h.getArtist)
		api.DELETE("/artists/:id", h.deleteArtist)
		api.PUT("/artists/:id/monitored", h.toggleMonitored)

		// Albums
		api.GET("/albums/:id", h.getAlbum)
		api.PUT("/albums/:id/download", h.downloadAlbum)

		// Library
		api.GET("/library/wanted", h.getWanted)
		api.POST("/library/scan", h.scanLibrary)
	}
}

// -----------------------------------------------------------------------------
// Search
// -----------------------------------------------------------------------------

// searchArtists searches MusicBrainz for artists matching a query
// GET /api/search?q=kendrick+lamar
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

// listArtists returns all monitored artists
// GET /api/artists
func (h *Handler) listArtists(c *gin.Context) {
	artists, err := h.db.GetAllArtists()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Attach genres to each artist
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

// addArtist adds a new artist to monitor by MusicBrainz ID
// POST /api/artists
// Body: { "musicbrainzId": "...", "monitored": true }
func (h *Handler) addArtist(c *gin.Context) {
	var req struct {
		MusicBrainzID string `json:"musicbrainzId" binding:"required"`
		Monitored     bool   `json:"monitored"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if artist already exists
	existing, _ := h.db.GetArtistByMBID(req.MusicBrainzID)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "artist already exists"})
		return
	}

	// Fetch artist details from MusicBrainz
	mbArtist, err := h.mb.GetArtist(req.MusicBrainzID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch artist from MusicBrainz"})
		return
	}

	// Create artist in database
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

	// Fetch artist image from Fanart.tv in the background
	go func() {
		imageURL, err := h.fanart.GetArtistImageURL(artist.MusicBrainzID)
		if err == nil && imageURL != "" {
			artist.ImageURL = imageURL
			h.db.UpdateArtist(artist)
		}
	}()

	// Fetch and save their discography in the background
	go h.syncDiscography(artist)

	c.JSON(http.StatusCreated, artist)
}

// getArtist returns a single artist with their albums
// GET /api/artists/:id
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

	// Attach albums
	albums, err := h.db.GetAlbumsByArtist(id)
	if err == nil {
		// Attach tracks to each album
		for i := range albums {
			tracks, _ := h.db.GetTracksByAlbum(albums[i].ID)
			albums[i].Tracks = tracks
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"artist": artist,
		"albums": albums,
	})
}

// deleteArtist removes an artist and all their data
// DELETE /api/artists/:id
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

// toggleMonitored enables or disables monitoring for an artist
// PUT /api/artists/:id/monitored
// Body: { "monitored": true }
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

// getAlbum returns a single album with its tracks
// GET /api/albums/:id
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

// downloadAlbum triggers a Soulseek search and download for an album
// PUT /api/albums/:id/download
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

	// Mark as downloading immediately so the UI updates
	if err := h.db.UpdateAlbumStatus(id, models.AlbumStatusDownloading); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Trigger the actual download in the background
	go func() {
		if err := h.downloader.DownloadAlbum(id); err != nil {
			log.Printf("[Karasu] Download failed for album %d: %v", id, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "download started", "albumId": id})
}

// -----------------------------------------------------------------------------
// Library
// -----------------------------------------------------------------------------

// getWanted returns all albums that are wanted but not yet downloaded
// GET /api/library/wanted
func (h *Handler) getWanted(c *gin.Context) {
	albums, err := h.db.GetAlbumsByStatus(models.AlbumStatusWanted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, albums)
}
// scanLibrary walks the music directory and reconciles files with the database
// marks tracks as downloaded if the files exist on disk
// POST /api/library/scan
func (h *Handler) scanLibrary(c *gin.Context) {
	scanner := library.NewScanner(h.db, os.Getenv("MUSIC_DIR"))
	result, err := scanner.Scan()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// -----------------------------------------------------------------------------
// Background jobs
// -----------------------------------------------------------------------------

// syncDiscography fetches an artist's full discography from MusicBrainz
// and saves it to the database. Runs in a goroutine.
func (h *Handler) syncDiscography(artist *models.Artist) {
	releaseGroups, err := h.mb.GetArtistReleaseGroups(artist.MusicBrainzID)
	if err != nil {
		return
	}

	for _, rg := range releaseGroups {
		// Skip non-album types we don't care about
		if rg.PrimaryType == "" {
			continue
		}

		// Parse release date — MusicBrainz dates can be partial e.g. "2015" or "2015-03"
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

		// Skip if album already exists
		existing, _ := h.db.GetAlbumByID(0) // placeholder
		_ = existing

		if err := h.db.CreateAlbum(album); err != nil {
			continue
		}
	}
}