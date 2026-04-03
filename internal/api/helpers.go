package api

import (
	"strings"
	"time"

	"karasu/internal/models"
)

// ParsePartialDate parses MusicBrainz dates which can be:
// "2015-03-15", "2015-03", "2015", or ""
func ParsePartialDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	formats := []string{
		"2006-01-02",
		"2006-01",
		"2006",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// NormalizeAlbumType converts MusicBrainz type strings to our AlbumType enum
func NormalizeAlbumType(t string) models.AlbumType {
	switch strings.ToLower(t) {
	case "album":
		return models.AlbumTypeAlbum
	case "ep":
		return models.AlbumTypeEP
	case "single":
		return models.AlbumTypeSingle
	case "live":
		return models.AlbumTypeLive
	case "compilation":
		return models.AlbumTypeCompilation
	default:
		return models.AlbumTypeAlbum
	}
}