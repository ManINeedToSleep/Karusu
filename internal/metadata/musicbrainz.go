	package metadata

	import (
		"encoding/json"
		"fmt"
		"net/http"
		"net/url"
		"time"
	)

	const (
		mbBaseURL   = "https://musicbrainz.org/ws/2"
		// MusicBrainz requires a descriptive User-Agent or they'll block you
		mbUserAgent = "Karasu/0.1 (https://github.com/ManINeedToSleep/Karasu)"
		// MusicBrainz rate limit — max 1 request per second
		mbRateLimit = time.Second
	)

	// MusicBrainzClient handles all communication with the MusicBrainz API
	type MusicBrainzClient struct {
		http      *http.Client
		lastCall  time.Time // tracks when we last made a request for rate limiting
	}

	// NewMusicBrainzClient creates a new client with a sensible timeout
	func NewMusicBrainzClient() *MusicBrainzClient {
		return &MusicBrainzClient{
			http: &http.Client{Timeout: 10 * time.Second},
		}
	}

	// rateLimit sleeps if we're calling too fast
	func (c *MusicBrainzClient) rateLimit() {
		elapsed := time.Since(c.lastCall)
		if elapsed < mbRateLimit {
			time.Sleep(mbRateLimit - elapsed)
		}
		c.lastCall = time.Now()
	}

	// get makes a GET request to MusicBrainz and decodes the JSON response
	func (c *MusicBrainzClient) get(endpoint string, params url.Values, out any) error {
		c.rateLimit()

		params.Set("fmt", "json") // always request JSON
		u := fmt.Sprintf("%s/%s?%s", mbBaseURL, endpoint, params.Encode())

		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		// MusicBrainz requires a User-Agent header
		req.Header.Set("User-Agent", mbUserAgent)

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("MusicBrainz returned status %d", resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}

		return nil
	}

	// -----------------------------------------------------------------------------
	// Artist Search
	// -----------------------------------------------------------------------------

	// MBArtist represents an artist from MusicBrainz
	type MBArtist struct {
		ID             string `json:"id"`           // MusicBrainz UUID
		Name           string `json:"name"`
		Disambiguation string `json:"disambiguation"` // e.g. "US rapper" to tell apart same-name artists
		Type           string `json:"type"`           // Person, Group, etc.
	}

	type mbArtistSearchResponse struct {
		Artists []MBArtist `json:"artists"`
	}

	// SearchArtists searches MusicBrainz for artists matching a query string
	func (c *MusicBrainzClient) SearchArtists(query string) ([]MBArtist, error) {
		params := url.Values{}
		params.Set("query", query)
		params.Set("limit", "10")

		var result mbArtistSearchResponse
		if err := c.get("artist", params, &result); err != nil {
			return nil, fmt.Errorf("artist search failed: %w", err)
		}

		return result.Artists, nil
	}

	// GetArtist fetches a single artist by their MusicBrainz ID
	func (c *MusicBrainzClient) GetArtist(mbid string) (*MBArtist, error) {
		params := url.Values{}

		var artist MBArtist
		if err := c.get(fmt.Sprintf("artist/%s", mbid), params, &artist); err != nil {
			return nil, fmt.Errorf("get artist failed: %w", err)
		}

		return &artist, nil
	}

	// -----------------------------------------------------------------------------
	// Release Groups (Albums)
	// -----------------------------------------------------------------------------

	// MBReleaseGroup represents an album/EP/single on MusicBrainz
	type MBReleaseGroup struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		PrimaryType string `json:"primary-type"` // Album, Single, EP, etc.
		FirstRelease string `json:"first-release-date"`
	}

	type mbReleaseGroupResponse struct {
		ReleaseGroups []MBReleaseGroup `json:"release-groups"`
	}

	// GetArtistReleaseGroups fetches all albums/EPs/singles for an artist
	func (c *MusicBrainzClient) GetArtistReleaseGroups(artistMBID string) ([]MBReleaseGroup, error) {
		params := url.Values{}
		params.Set("artist", artistMBID)
		params.Set("limit", "100")

		var result mbReleaseGroupResponse
		if err := c.get("release-group", params, &result); err != nil {
			return nil, fmt.Errorf("get release groups failed: %w", err)
		}

		return result.ReleaseGroups, nil
	}

	// -----------------------------------------------------------------------------
	// Releases (specific editions of an album with track listings)
	// -----------------------------------------------------------------------------

	// MBTrack represents a track on a release
	type MBTrack struct {
		ID       string `json:"id"`
		Number   string `json:"number"`
		Title    string `json:"title"`
		Length   int    `json:"length"` // duration in milliseconds
	}

	// MBMedium represents a disc in a release (most albums have 1)
	type MBMedium struct {
		Position int       `json:"position"`
		Tracks   []MBTrack `json:"tracks"`
	}

	// MBRelease represents a specific release (edition) of an album
	type MBRelease struct {
		ID      string     `json:"id"`
		Title   string     `json:"title"`
		Date    string     `json:"date"`
		Media   []MBMedium `json:"media"`
	}

	type mbReleaseResponse struct {
		Releases []MBRelease `json:"releases"`
	}

	// GetReleaseGroupReleases fetches specific releases (editions) of an album, with track listings
	func (c *MusicBrainzClient) GetReleaseGroupReleases(releaseGroupMBID string) ([]MBRelease, error) {
		params := url.Values{}
		params.Set("release-group", releaseGroupMBID)
		params.Set("inc", "recordings+media") // include track listings
		params.Set("limit", "10")

		var result mbReleaseResponse
		if err := c.get("release", params, &result); err != nil {
			return nil, fmt.Errorf("get releases failed: %w", err)
		}

		return result.Releases, nil
	}

	// GetCoverArtURL returns the cover art URL for a release group
	// Uses the Cover Art Archive — free, no API key needed
	// Uses front-250 which is a small thumbnail, perfect for grid display
	func GetCoverArtURL(releaseGroupMBID string) string {
		return fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-250", releaseGroupMBID)
	}