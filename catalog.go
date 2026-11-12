package spotigo

import (
	"context"
	"net/url"
)

func path(parts ...string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += "/"
		}
		if i%2 == 1 {
			p = url.PathEscape(p)
		}
		s += p
	}
	return s
}

func getOne[T any](ctx context.Context, c *Client, p string, q url.Values) (*T, error) {
	var v T
	if err := c.get(ctx, p, q, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// getField fetches p and returns the value stored under key in a
// single-key envelope such as {"artists": [...]}.
func getField[T any](ctx context.Context, c *Client, p string, q url.Values, key string) (T, error) {
	var v map[string]T
	err := c.get(ctx, p, q, &v)
	return v[key], err
}

// getFieldPtr is like getField but returns a pointer, or nil on error.
func getFieldPtr[T any](ctx context.Context, c *Client, p string, q url.Values, key string) (*T, error) {
	v, err := getField[T](ctx, c, p, q, key)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func getPage[T any](ctx context.Context, c *Client, p string, q url.Values) (*Page[T], error) {
	return getOne[Page[T]](ctx, c, p, q)
}

// ---- Albums ----

// GetAlbum returns catalog information for a single album.
func (c *Client) GetAlbum(ctx context.Context, id ID, opts ...RequestOption) (*FullAlbum, error) {
	return getOne[FullAlbum](ctx, c, path("albums", string(id)), c.paramsMarket(opts...))
}

// GetAlbums returns several albums in the order requested. Any number of IDs
// is accepted: requests are chunked (20 per call) or fanned out (see BatchMode).
// Unknown IDs produce nil entries.
func (c *Client) GetAlbums(ctx context.Context, ids []ID, opts ...RequestOption) ([]*FullAlbum, error) {
	return getMany[FullAlbum](ctx, c, "albums", ids, albumBatch, c.paramsMarket(opts...))
}

// GetAlbumTracks returns a page of an album's tracks.
func (c *Client) GetAlbumTracks(ctx context.Context, id ID, opts ...RequestOption) (*Page[SimpleTrack], error) {
	return getPage[SimpleTrack](ctx, c, path("albums", string(id), "tracks"), c.paramsMarket(opts...))
}

// ---- Artists ----

// GetArtist returns a single artist.
func (c *Client) GetArtist(ctx context.Context, id ID) (*FullArtist, error) {
	return getOne[FullArtist](ctx, c, path("artists", string(id)), nil)
}

// GetArtists returns several artists (chunked by 50 or fanned out).
func (c *Client) GetArtists(ctx context.Context, ids ...ID) ([]*FullArtist, error) {
	return getMany[FullArtist](ctx, c, "artists", ids, catalogBatch, nil)
}

// GetArtistAlbums returns a page of an artist's albums. Use IncludeGroups to filter.
func (c *Client) GetArtistAlbums(ctx context.Context, id ID, opts ...RequestOption) (*Page[SimpleAlbum], error) {
	return getPage[SimpleAlbum](ctx, c, path("artists", string(id), "albums"), c.paramsMarket(opts...))
}

// GetArtistTopTracks returns an artist's top tracks in a market.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) GetArtistTopTracks(ctx context.Context, id ID, opts ...RequestOption) ([]FullTrack, error) {
	return getField[[]FullTrack](ctx, c, path("artists", string(id), "top-tracks"), c.paramsMarket(opts...), "tracks")
}

// GetRelatedArtists returns artists similar to the given one.
//
// Deprecated: unavailable to new apps since November 2024.
func (c *Client) GetRelatedArtists(ctx context.Context, id ID) ([]FullArtist, error) {
	return getField[[]FullArtist](ctx, c, path("artists", string(id), "related-artists"), nil, "artists")
}

// ---- Tracks ----

// GetTrack returns a single track.
func (c *Client) GetTrack(ctx context.Context, id ID, opts ...RequestOption) (*FullTrack, error) {
	return getOne[FullTrack](ctx, c, path("tracks", string(id)), c.paramsMarket(opts...))
}

// GetTracks returns several tracks (chunked by 50 or fanned out).
func (c *Client) GetTracks(ctx context.Context, ids []ID, opts ...RequestOption) ([]*FullTrack, error) {
	return getMany[FullTrack](ctx, c, "tracks", ids, catalogBatch, c.paramsMarket(opts...))
}

// ---- Shows & episodes ----

// GetShow returns a single show.
func (c *Client) GetShow(ctx context.Context, id ID, opts ...RequestOption) (*FullShow, error) {
	return getOne[FullShow](ctx, c, path("shows", string(id)), c.paramsMarket(opts...))
}

// GetShows returns several shows (chunked by 50 or fanned out).
func (c *Client) GetShows(ctx context.Context, ids []ID, opts ...RequestOption) ([]*SimpleShow, error) {
	return getMany[SimpleShow](ctx, c, "shows", ids, catalogBatch, c.paramsMarket(opts...))
}

// GetShowEpisodes returns a page of a show's episodes.
func (c *Client) GetShowEpisodes(ctx context.Context, id ID, opts ...RequestOption) (*Page[SimpleEpisode], error) {
	return getPage[SimpleEpisode](ctx, c, path("shows", string(id), "episodes"), c.paramsMarket(opts...))
}

// GetEpisode returns a single episode.
func (c *Client) GetEpisode(ctx context.Context, id ID, opts ...RequestOption) (*FullEpisode, error) {
	return getOne[FullEpisode](ctx, c, path("episodes", string(id)), c.paramsMarket(opts...))
}

// GetEpisodes returns several episodes (chunked by 50 or fanned out).
func (c *Client) GetEpisodes(ctx context.Context, ids []ID, opts ...RequestOption) ([]*FullEpisode, error) {
	return getMany[FullEpisode](ctx, c, "episodes", ids, catalogBatch, c.paramsMarket(opts...))
}

// ---- Audiobooks & chapters ----

// GetAudiobook returns a single audiobook.
func (c *Client) GetAudiobook(ctx context.Context, id ID, opts ...RequestOption) (*FullAudiobook, error) {
	return getOne[FullAudiobook](ctx, c, path("audiobooks", string(id)), c.paramsMarket(opts...))
}

// GetAudiobooks returns several audiobooks (chunked by 50 or fanned out).
func (c *Client) GetAudiobooks(ctx context.Context, ids []ID, opts ...RequestOption) ([]*FullAudiobook, error) {
	return getMany[FullAudiobook](ctx, c, "audiobooks", ids, catalogBatch, c.paramsMarket(opts...))
}

// GetAudiobookChapters returns a page of an audiobook's chapters.
func (c *Client) GetAudiobookChapters(ctx context.Context, id ID, opts ...RequestOption) (*Page[SimpleChapter], error) {
	return getPage[SimpleChapter](ctx, c, path("audiobooks", string(id), "chapters"), c.paramsMarket(opts...))
}

// GetChapter returns a single chapter.
func (c *Client) GetChapter(ctx context.Context, id ID, opts ...RequestOption) (*FullChapter, error) {
	return getOne[FullChapter](ctx, c, path("chapters", string(id)), c.paramsMarket(opts...))
}

// GetChapters returns several chapters (chunked by 50 or fanned out).
func (c *Client) GetChapters(ctx context.Context, ids []ID, opts ...RequestOption) ([]*FullChapter, error) {
	return getMany[FullChapter](ctx, c, "chapters", ids, catalogBatch, c.paramsMarket(opts...))
}
