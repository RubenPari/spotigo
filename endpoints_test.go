package spotigo

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestEndpointRouting checks that every wrapper hits the documented
// method + path (including the 2026 /me/library and /playlists/{id}/items routes).
func TestEndpointRouting(t *testing.T) {
	var got string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + strings.TrimPrefix(r.URL.Path, "/v1")
		if q := r.URL.Query(); len(q) > 0 {
			got += "?" + q.Encode()
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/contains"):
			writeJSON(w, 200, []bool{true})
		case strings.HasSuffix(r.URL.Path, "/images") && r.Method == http.MethodGet:
			writeJSON(w, 200, []any{})
		default:
			writeJSON(w, 200, map[string]any{})
		}
	}, WithBatchMode(BatchFanOut))
	ctx := context.Background()
	cases := []struct {
		want string
		call func() error
	}{
		{"GET /albums/a", func() error { _, err := c.GetAlbum(ctx, "a"); return err }},
		{"GET /albums/a/tracks?limit=5", func() error { _, err := c.GetAlbumTracks(ctx, "a", Limit(5)); return err }},
		{"GET /artists/a", func() error { _, err := c.GetArtist(ctx, "a"); return err }},
		{"GET /artists/a/albums?include_groups=album%2Csingle", func() error {
			_, err := c.GetArtistAlbums(ctx, "a", IncludeGroups("album", "single"))
			return err
		}},
		{"GET /tracks/x", func() error { _, err := c.GetTracks(ctx, []ID{"x"}); return err }},
		{"GET /shows/s", func() error { _, err := c.GetShow(ctx, "s"); return err }},
		{"GET /shows/s/episodes", func() error { _, err := c.GetShowEpisodes(ctx, "s"); return err }},
		{"GET /episodes/e", func() error { _, err := c.GetEpisode(ctx, "e"); return err }},
		{"GET /audiobooks/b", func() error { _, err := c.GetAudiobook(ctx, "b"); return err }},
		{"GET /audiobooks/b/chapters", func() error { _, err := c.GetAudiobookChapters(ctx, "b"); return err }},
		{"GET /chapters/c", func() error { _, err := c.GetChapter(ctx, "c"); return err }},
		{"GET /search?limit=3&q=x&type=track%2Calbum", func() error {
			_, err := c.Search(ctx, "x", []ItemType{TypeTrack, TypeAlbum}, Limit(3))
			return err
		}},
		{"GET /me", func() error { _, err := c.CurrentUser(ctx); return err }},
		{"GET /me/top/tracks?time_range=short_term", func() error { _, err := c.TopTracks(ctx, Range(ShortTerm)); return err }},
		{"GET /me/top/artists", func() error { _, err := c.TopArtists(ctx); return err }},
		{"GET /me/following?limit=50&type=artist", func() error { _, err := c.FollowedArtists(ctx, Limit(50)); return err }},
		{"GET /me/albums", func() error { _, err := c.SavedAlbums(ctx); return err }},
		{"GET /me/shows", func() error { _, err := c.SavedShows(ctx); return err }},
		{"GET /me/episodes", func() error { _, err := c.SavedEpisodes(ctx); return err }},
		{"GET /me/audiobooks", func() error { _, err := c.SavedAudiobooks(ctx); return err }},
		{"PUT /me/library?uris=spotify%3Aalbum%3Aa", func() error { return c.SaveAlbums(ctx, "a") }},
		{"DELETE /me/library?uris=spotify%3Ashow%3As", func() error { return c.RemoveShows(ctx, "s") }},
		{"PUT /me/library?uris=spotify%3Aplaylist%3Ap", func() error { return c.FollowPlaylist(ctx, "p") }},
		{"DELETE /me/library?uris=spotify%3Auser%3Abob", func() error { return c.UnfollowUsers(ctx, "bob") }},
		{"GET /me/library/contains?uris=spotify%3Aartist%3Ax", func() error { _, err := c.FollowsArtists(ctx, "x"); return err }},
		{"GET /playlists/p", func() error { _, err := c.GetPlaylist(ctx, "p"); return err }},
		{"GET /playlists/p/items?limit=50", func() error { _, err := c.PlaylistItems(ctx, "p", Limit(50)); return err }},
		{"GET /me/playlists", func() error { _, err := c.CurrentUserPlaylists(ctx); return err }},
		{"POST /me/playlists", func() error { _, err := c.CreatePlaylist(ctx, PlaylistDetails{Name: "n"}); return err }},
		{"PUT /playlists/p", func() error { return c.UpdatePlaylist(ctx, "p", PlaylistDetails{Name: "n"}) }},
		{"DELETE /playlists/p/items", func() error { _, err := c.RemoveItems(ctx, "p", "", "spotify:track:t"); return err }},
		{"PUT /playlists/p/items", func() error { _, err := c.ReorderItems(ctx, "p", Reorder{RangeStart: 1}); return err }},
		{"GET /playlists/p/images", func() error { _, err := c.PlaylistCover(ctx, "p"); return err }},
		{"PUT /playlists/p/images", func() error { return c.UploadPlaylistCover(ctx, "p", strings.NewReader("jpeg")) }},
		{"GET /me/player/devices", func() error { _, err := c.Devices(ctx); return err }},
		{"PUT /me/player", func() error { return c.TransferPlayback(ctx, "d", true) }},
		{"POST /me/player/next?device_id=d", func() error { return c.Next(ctx, "d") }},
		{"POST /me/player/previous", func() error { return c.Previous(ctx, "") }},
		{"PUT /me/player/seek?position_ms=2000", func() error { return c.Seek(ctx, 2*time.Second, "") }},
		{"PUT /me/player/repeat?state=track", func() error { return c.SetRepeat(ctx, RepeatTrack, "") }},
		{"PUT /me/player/volume?volume_percent=100", func() error { return c.SetVolume(ctx, 150, "") }},
		{"PUT /me/player/shuffle?state=true", func() error { return c.SetShuffle(ctx, true, "") }},
		{"GET /me/player/queue", func() error { _, err := c.Queue(ctx); return err }},
		{"POST /me/player/queue?uri=spotify%3Atrack%3At", func() error { return c.AddToQueue(ctx, "spotify:track:t", "") }},
		{"GET /me/player/recently-played?limit=10", func() error { _, err := c.RecentlyPlayed(ctx, Limit(10)); return err }},
		{"GET /me/player/currently-playing?additional_types=track%2Cepisode", func() error {
			_, err := c.CurrentlyPlaying(ctx, AdditionalTypes(TypeTrack, TypeEpisode))
			return err
		}},
	}
	for _, tc := range cases {
		got = ""
		if err := tc.call(); err != nil {
			t.Errorf("%s: %v", tc.want, err)
			continue
		}
		if got != tc.want {
			t.Errorf("want %q, got %q", tc.want, got)
		}
	}
}
