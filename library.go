package spotigo

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/url"
)

// SaveToLibrary saves any mix of tracks, albums, episodes, shows,
// audiobooks, playlists (follow) and users (follow) to the current user's
// library via the unified PUT /me/library endpoint (2026 API). Any number of
// URIs is accepted; requests are chunked automatically.
func (c *Client) SaveToLibrary(ctx context.Context, uris ...URI) error {
	return c.libraryWrite(ctx, http.MethodPut, uris)
}

// RemoveFromLibrary is the inverse of SaveToLibrary (DELETE /me/library).
func (c *Client) RemoveFromLibrary(ctx context.Context, uris ...URI) error {
	return c.libraryWrite(ctx, http.MethodDelete, uris)
}

func (c *Client) libraryWrite(ctx context.Context, method string, uris []URI) error {
	for _, part := range chunk(uris, libraryChunk) {
		q := url.Values{"uris": {joinCSV(part)}}
		if err := c.do(ctx, &request{method: method, path: "me/library", query: q}, nil); err != nil {
			return err
		}
	}
	return nil
}

// LibraryContains reports, for each URI, whether it is saved/followed by the
// current user (GET /me/library/contains). Accepts artists too.
func (c *Client) LibraryContains(ctx context.Context, uris ...URI) ([]bool, error) {
	out := make([]bool, 0, len(uris))
	for _, part := range chunk(uris, libraryChunk) {
		var res []bool
		if err := c.get(ctx, "me/library/contains", url.Values{"uris": {joinCSV(part)}}, &res); err != nil {
			return nil, err
		}
		out = append(out, res...)
	}
	return out, nil
}

// Typed convenience wrappers.

// SaveTracks saves tracks to "Liked Songs".
func (c *Client) SaveTracks(ctx context.Context, ids ...ID) error {
	return c.SaveToLibrary(ctx, toURIs(TypeTrack, ids)...)
}

// RemoveTracks removes tracks from "Liked Songs".
func (c *Client) RemoveTracks(ctx context.Context, ids ...ID) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeTrack, ids)...)
}

// HasTracks reports whether tracks are in "Liked Songs".
func (c *Client) HasTracks(ctx context.Context, ids ...ID) ([]bool, error) {
	return c.LibraryContains(ctx, toURIs(TypeTrack, ids)...)
}

// SaveAlbums saves albums to the library.
func (c *Client) SaveAlbums(ctx context.Context, ids ...ID) error {
	return c.SaveToLibrary(ctx, toURIs(TypeAlbum, ids)...)
}

// RemoveAlbums removes albums from the library.
func (c *Client) RemoveAlbums(ctx context.Context, ids ...ID) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeAlbum, ids)...)
}

// HasAlbums reports whether albums are saved.
func (c *Client) HasAlbums(ctx context.Context, ids ...ID) ([]bool, error) {
	return c.LibraryContains(ctx, toURIs(TypeAlbum, ids)...)
}

// SaveShows saves shows to the library.
func (c *Client) SaveShows(ctx context.Context, ids ...ID) error {
	return c.SaveToLibrary(ctx, toURIs(TypeShow, ids)...)
}

// RemoveShows removes shows from the library.
func (c *Client) RemoveShows(ctx context.Context, ids ...ID) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeShow, ids)...)
}

// SaveEpisodes saves episodes to the library.
func (c *Client) SaveEpisodes(ctx context.Context, ids ...ID) error {
	return c.SaveToLibrary(ctx, toURIs(TypeEpisode, ids)...)
}

// RemoveEpisodes removes episodes from the library.
func (c *Client) RemoveEpisodes(ctx context.Context, ids ...ID) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeEpisode, ids)...)
}

// SaveAudiobooks saves audiobooks to the library.
func (c *Client) SaveAudiobooks(ctx context.Context, ids ...ID) error {
	return c.SaveToLibrary(ctx, toURIs(TypeAudiobook, ids)...)
}

// RemoveAudiobooks removes audiobooks from the library.
func (c *Client) RemoveAudiobooks(ctx context.Context, ids ...ID) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeAudiobook, ids)...)
}

// FollowPlaylist follows a playlist.
func (c *Client) FollowPlaylist(ctx context.Context, id ID) error {
	return c.SaveToLibrary(ctx, NewURI(TypePlaylist, id))
}

// UnfollowPlaylist unfollows a playlist.
func (c *Client) UnfollowPlaylist(ctx context.Context, id ID) error {
	return c.RemoveFromLibrary(ctx, NewURI(TypePlaylist, id))
}

// FollowUsers follows users.
func (c *Client) FollowUsers(ctx context.Context, ids ...string) error {
	return c.SaveToLibrary(ctx, toURIs(TypeUser, ids)...)
}

// UnfollowUsers unfollows users.
func (c *Client) UnfollowUsers(ctx context.Context, ids ...string) error {
	return c.RemoveFromLibrary(ctx, toURIs(TypeUser, ids)...)
}

// FollowArtists follows artists. It uses /me/library and falls back to the
// legacy PUT /me/following endpoint if the former rejects artist URIs.
func (c *Client) FollowArtists(ctx context.Context, ids ...ID) error {
	err := c.SaveToLibrary(ctx, toURIs(TypeArtist, ids)...)
	if errors.Is(err, ErrBadRequest) {
		return c.legacyFollow(ctx, http.MethodPut, ids)
	}
	return err
}

// UnfollowArtists unfollows artists (see FollowArtists for fallback rules).
func (c *Client) UnfollowArtists(ctx context.Context, ids ...ID) error {
	err := c.RemoveFromLibrary(ctx, toURIs(TypeArtist, ids)...)
	if errors.Is(err, ErrBadRequest) {
		return c.legacyFollow(ctx, http.MethodDelete, ids)
	}
	return err
}

func (c *Client) legacyFollow(ctx context.Context, method string, ids []ID) error {
	for _, part := range chunk(ids, followChunk) {
		q := url.Values{"type": {"artist"}, "ids": {joinCSV(part)}}
		if err := c.do(ctx, &request{method: method, path: "me/following", query: q}, nil); err != nil {
			return err
		}
	}
	return nil
}

// FollowsArtists reports whether the user follows the given artists.
func (c *Client) FollowsArtists(ctx context.Context, ids ...ID) ([]bool, error) {
	return c.LibraryContains(ctx, toURIs(TypeArtist, ids)...)
}

// ---- Saved item listings ----

// SavedTracks returns a page of the user's "Liked Songs".
func (c *Client) SavedTracks(ctx context.Context, opts ...RequestOption) (*Page[SavedTrack], error) {
	return getPage[SavedTrack](ctx, c, "me/tracks", c.paramsMarket(opts...))
}

// AllSavedTracks iterates over all of the user's "Liked Songs" (50 per request).
func (c *Client) AllSavedTracks(ctx context.Context, opts ...RequestOption) iter.Seq2[SavedTrack, error] {
	p, err := c.SavedTracks(ctx, withMaxLimit(opts)...)
	return Paginate(ctx, c, p, err)
}

// SavedAlbums returns a page of the user's saved albums.
func (c *Client) SavedAlbums(ctx context.Context, opts ...RequestOption) (*Page[SavedAlbum], error) {
	return getPage[SavedAlbum](ctx, c, "me/albums", c.paramsMarket(opts...))
}

// AllSavedAlbums iterates over all saved albums.
func (c *Client) AllSavedAlbums(ctx context.Context, opts ...RequestOption) iter.Seq2[SavedAlbum, error] {
	p, err := c.SavedAlbums(ctx, withMaxLimit(opts)...)
	return Paginate(ctx, c, p, err)
}

// SavedShows returns a page of the user's saved shows.
func (c *Client) SavedShows(ctx context.Context, opts ...RequestOption) (*Page[SavedShow], error) {
	return getPage[SavedShow](ctx, c, "me/shows", c.params(opts...))
}

// SavedEpisodes returns a page of the user's saved episodes.
func (c *Client) SavedEpisodes(ctx context.Context, opts ...RequestOption) (*Page[SavedEpisode], error) {
	return getPage[SavedEpisode](ctx, c, "me/episodes", c.paramsMarket(opts...))
}

// SavedAudiobooks returns a page of the user's saved audiobooks.
func (c *Client) SavedAudiobooks(ctx context.Context, opts ...RequestOption) (*Page[SimpleAudiobook], error) {
	return getPage[SimpleAudiobook](ctx, c, "me/audiobooks", c.params(opts...))
}
