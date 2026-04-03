package metadata

import (
	"fmt"
	"testing"
)

func TestSearchArtists(t *testing.T) {
	client := NewMusicBrainzClient()

	artists, err := client.SearchArtists("Kendrick Lamar")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	for _, a := range artists {
		fmt.Printf("ID: %s | Name: %s | Type: %s | Note: %s\n",
			a.ID, a.Name, a.Type, a.Disambiguation)
	}
}

func TestGetDiscography(t *testing.T) {
	client := NewMusicBrainzClient()

	// Kendrick Lamar's real MBID from the search above
	albums, err := client.GetArtistReleaseGroups("381086ea-f511-4aba-bdf9-71c753dc5077")
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	for _, a := range albums {
		fmt.Printf("Type: %-12s | Date: %-12s | Title: %s\n",
			a.PrimaryType, a.FirstRelease, a.Title)
	}
}