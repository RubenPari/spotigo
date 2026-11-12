package spotigo_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/RubenPari/spotigo"
	"github.com/RubenPari/spotigo/auth"
)

// App-only access with client credentials (catalog data only).
func Example_clientCredentials() {
	ctx := context.Background()
	c := spotigo.New(auth.AppClient(ctx, "", "")) // reads SPOTIFY_ID / SPOTIFY_SECRET

	// Any URI or open.spotify.com link works.
	_, id, _ := spotigo.ParseURI("https://open.spotify.com/intl-it/track/6rqhFgbbKwnb9MLmUQDhG6")
	t, err := c.GetTrack(ctx, id)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(t.Name, "by", spotigo.ArtistNames(t.Artists))
}

// Iterate over every liked song with a range-over-func loop.
func Example_iterators() {
	ctx := context.Background()
	a := auth.New(auth.WithRedirectURL("http://127.0.0.1:8888/callback"), auth.WithScopes(auth.ScopeUserLibraryRead))
	store, _ := auth.DefaultFileStore("example")
	tok, err := a.LocalLogin(ctx, auth.LoginOptions{Store: store})
	if err != nil {
		log.Fatal(err)
	}
	c := spotigo.New(a.PersistentClient(ctx, tok, store))

	for saved, err := range c.AllSavedTracks(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(saved.AddedAt.Format("2006-01-02"), saved.Track.Name)
	}
}

// Typed errors work with errors.Is.
func Example_errors() {
	var c *spotigo.Client // authenticated client
	ctx := context.Background()
	err := c.Pause(ctx, "")
	switch {
	case errors.Is(err, spotigo.ErrNoActiveDevice):
		fmt.Println("open Spotify somewhere first")
	case errors.Is(err, spotigo.ErrPremiumRequired):
		fmt.Println("playback control needs Premium")
	case errors.Is(err, spotigo.ErrRateLimited):
		e, _ := spotigo.AsError(err)
		fmt.Println("retry after", e.RetryAfter)
	}
}

// Build a playlist from a search, any size.
func Example_playlist() {
	var c *spotigo.Client // authenticated client
	ctx := context.Background()

	pl, err := c.CreatePlaylist(ctx, spotigo.PlaylistDetails{Name: "Daft Punk, all of it", Public: spotigo.Ptr(false)})
	if err != nil {
		log.Fatal(err)
	}
	var uris []spotigo.URI
	for t, err := range spotigo.Take(c.SearchTracks(ctx, spotigo.Query{Artist: "Daft Punk"}.String()), 300) {
		if err != nil {
			log.Fatal(err)
		}
		uris = append(uris, t.URI)
	}
	// 300 URIs: chunked into 3 requests automatically.
	if _, err := c.AddItems(ctx, pl.ID, uris...); err != nil {
		log.Fatal(err)
	}
}
