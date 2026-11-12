package spotigo

import (
	"context"
	"iter"
	"strings"
)

// SearchResult holds one page per requested type. Fields for types not
// requested are nil.
type SearchResult struct {
	Tracks     *Page[FullTrack]        `json:"tracks"`
	Artists    *Page[FullArtist]       `json:"artists"`
	Albums     *Page[SimpleAlbum]      `json:"albums"`
	Playlists  *Page[*SimplePlaylist]  `json:"playlists"` // entries may be nil
	Shows      *Page[*SimpleShow]      `json:"shows"`     // entries may be nil
	Episodes   *Page[*SimpleEpisode]   `json:"episodes"`  // entries may be nil
	Audiobooks *Page[*SimpleAudiobook] `json:"audiobooks"`
}

// Search queries the catalog for one or more item types.
//
// Since February 2026 Development Mode apps get at most 10 results per page
// (default 5); use the Search* iterators to transparently page through more.
func (c *Client) Search(ctx context.Context, query string, types []ItemType, opts ...RequestOption) (*SearchResult, error) {
	q := c.paramsMarket(opts...)
	q.Set("q", query)
	q.Set("type", joinCSV(types))
	return getOne[SearchResult](ctx, c, "search", q)
}

func searchIter[T any](ctx context.Context, c *Client, query string, t ItemType, pick func(*SearchResult) *Page[T], opts []RequestOption) iter.Seq2[T, error] {
	res, err := c.Search(ctx, query, []ItemType{t}, opts...)
	var first *Page[T]
	if err == nil {
		first = pick(res)
	}
	return Paginate(ctx, c, first, err)
}

// SearchTracks iterates over every track matching query.
func (c *Client) SearchTracks(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[FullTrack, error] {
	return searchIter(ctx, c, query, TypeTrack, func(r *SearchResult) *Page[FullTrack] { return r.Tracks }, opts)
}

// SearchArtists iterates over every artist matching query.
func (c *Client) SearchArtists(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[FullArtist, error] {
	return searchIter(ctx, c, query, TypeArtist, func(r *SearchResult) *Page[FullArtist] { return r.Artists }, opts)
}

// SearchAlbums iterates over every album matching query.
func (c *Client) SearchAlbums(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[SimpleAlbum, error] {
	return searchIter(ctx, c, query, TypeAlbum, func(r *SearchResult) *Page[SimpleAlbum] { return r.Albums }, opts)
}

// SearchPlaylists iterates over every playlist matching query, skipping the
// null entries Spotify sometimes returns.
func (c *Client) SearchPlaylists(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[SimplePlaylist, error] {
	return skipNil(searchIter(ctx, c, query, TypePlaylist, func(r *SearchResult) *Page[*SimplePlaylist] { return r.Playlists }, opts))
}

// SearchShows iterates over every show matching query.
func (c *Client) SearchShows(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[SimpleShow, error] {
	return skipNil(searchIter(ctx, c, query, TypeShow, func(r *SearchResult) *Page[*SimpleShow] { return r.Shows }, opts))
}

// SearchEpisodes iterates over every episode matching query.
func (c *Client) SearchEpisodes(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[SimpleEpisode, error] {
	return skipNil(searchIter(ctx, c, query, TypeEpisode, func(r *SearchResult) *Page[*SimpleEpisode] { return r.Episodes }, opts))
}

// SearchAudiobooks iterates over every audiobook matching query.
func (c *Client) SearchAudiobooks(ctx context.Context, query string, opts ...RequestOption) iter.Seq2[SimpleAudiobook, error] {
	return skipNil(searchIter(ctx, c, query, TypeAudiobook, func(r *SearchResult) *Page[*SimpleAudiobook] { return r.Audiobooks }, opts))
}

func skipNil[T any](seq iter.Seq2[*T, error]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		for v, err := range seq {
			if err != nil {
				yield(zero, err)
				return
			}
			if v == nil {
				continue
			}
			if !yield(*v, nil) {
				return
			}
		}
	}
}

// Query is a builder for Spotify's field-filter search syntax.
//
//	spotigo.Query{Artist: "Daft Punk", Year: "2001"}.String()
//	// => artist:"Daft Punk" year:2001
type Query struct {
	Text   string // free text
	Artist string
	Album  string
	Track  string
	Year   string // "1999" or range "1990-1999"
	Genre  string
	ISRC   string
	UPC    string
	// Tag can be "new" (albums released in the past two weeks) or
	// "hipster" (lowest 10% popularity).
	Tag string
}

func (q Query) String() string {
	var parts []string
	add := func(k, v string) {
		if v == "" {
			return
		}
		if strings.ContainsAny(v, " \t") {
			v = `"` + strings.ReplaceAll(v, `"`, "") + `"`
		}
		parts = append(parts, k+":"+v)
	}
	if q.Text != "" {
		parts = append(parts, q.Text)
	}
	add("artist", q.Artist)
	add("album", q.Album)
	add("track", q.Track)
	add("year", q.Year)
	add("genre", q.Genre)
	add("isrc", q.ISRC)
	add("upc", q.UPC)
	add("tag", q.Tag)
	return strings.Join(parts, " ")
}
