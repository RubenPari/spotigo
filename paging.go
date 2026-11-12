package spotigo

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
)

// ErrNoMorePages is returned by NextPage / PreviousPage at the boundaries.
var ErrNoMorePages = errors.New("spotigo: no more pages")

// Page is Spotify's offset-based paging object.
type Page[T any] struct {
	Href     string `json:"href"`
	Items    []T    `json:"items"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	Total    int    `json:"total"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
}

// HasNext reports whether there is a following page.
func (p *Page[T]) HasNext() bool { return p != nil && p.Next != "" }

// Cursors used by cursor-based paging.
type Cursors struct {
	After  string `json:"after"`
	Before string `json:"before"`
}

// CursorPage is Spotify's cursor-based paging object.
type CursorPage[T any] struct {
	Href    string  `json:"href"`
	Items   []T     `json:"items"`
	Limit   int     `json:"limit"`
	Next    string  `json:"next"`
	Cursors Cursors `json:"cursors"`
	Total   int     `json:"total"`
}

// HasNext reports whether there is a following page.
func (p *CursorPage[T]) HasNext() bool { return p != nil && p.Next != "" }

// fetchPage fetches href and decodes it into out, unwrapping single-key
// envelopes such as {"tracks": {...}} returned by search and some library
// endpoints.
func fetchPage(ctx context.Context, c *Client, href string, out any) error {
	var raw json.RawMessage
	if err := c.get(ctx, href, nil, &raw); err != nil {
		return err
	}
	return unwrapPage(raw, out)
}

func unwrapPage(raw json.RawMessage, out any) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return err
	}
	if _, ok := probe["items"]; !ok && len(probe) == 1 {
		for _, v := range probe {
			raw = v
		}
	}
	return json.Unmarshal(raw, out)
}

// fetchNext fetches the page at href (a "next" or "previous" link).
func fetchNext[P any](ctx context.Context, c *Client, href string) (*P, error) {
	var out P
	if err := fetchPage(ctx, c, href, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NextPage fetches the page following p.
func NextPage[T any](ctx context.Context, c *Client, p *Page[T]) (*Page[T], error) {
	if !p.HasNext() {
		return nil, ErrNoMorePages
	}
	return fetchNext[Page[T]](ctx, c, p.Next)
}

// PreviousPage fetches the page preceding p.
func PreviousPage[T any](ctx context.Context, c *Client, p *Page[T]) (*Page[T], error) {
	if p == nil || p.Previous == "" {
		return nil, ErrNoMorePages
	}
	return fetchNext[Page[T]](ctx, c, p.Previous)
}

// NextCursorPage fetches the page following p.
func NextCursorPage[T any](ctx context.Context, c *Client, p *CursorPage[T]) (*CursorPage[T], error) {
	if !p.HasNext() {
		return nil, ErrNoMorePages
	}
	return fetchNext[CursorPage[T]](ctx, c, p.Next)
}

// paginate is the shared engine behind Paginate and PaginateCursor: it
// yields the items of every page, following next links until one is empty.
func paginate[T, P any](ctx context.Context, c *Client, first *P, err error, items func(*P) []T, next func(*P) string) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		if err != nil {
			yield(zero, err)
			return
		}
		for p := first; p != nil; {
			for _, it := range items(p) {
				if !yield(it, nil) {
					return
				}
			}
			href := next(p)
			if href == "" {
				return
			}
			np, err := fetchNext[P](ctx, c, href)
			if err != nil {
				yield(zero, err)
				return
			}
			p = np
		}
	}
}

// Paginate iterates over every item starting from first, transparently
// fetching subsequent pages:
//
//	page, err := client.SavedTracks(ctx, spotigo.Limit(50))
//	for t, err := range spotigo.Paginate(ctx, client, page, err) {
//		if err != nil { return err }
//		fmt.Println(t.Track.Name)
//	}
//
// The err argument lets you pass the result of the first call directly.
func Paginate[T any](ctx context.Context, c *Client, first *Page[T], err error) iter.Seq2[T, error] {
	return paginate(ctx, c, first, err,
		func(p *Page[T]) []T { return p.Items },
		func(p *Page[T]) string { return p.Next })
}

// PaginateCursor is the cursor-paged counterpart of Paginate.
func PaginateCursor[T any](ctx context.Context, c *Client, first *CursorPage[T], err error) iter.Seq2[T, error] {
	return paginate(ctx, c, first, err,
		func(p *CursorPage[T]) []T { return p.Items },
		func(p *CursorPage[T]) string { return p.Next })
}

// withMaxLimit prepends Limit(maxPageLimit) so All* iterators fetch as few
// pages as possible; an explicit Limit in opts still wins.
func withMaxLimit(opts []RequestOption) []RequestOption {
	return append([]RequestOption{Limit(maxPageLimit)}, opts...)
}

// Collect drains an iterator into a slice, stopping at the first error.
func Collect[T any](seq iter.Seq2[T, error]) ([]T, error) {
	var out []T
	for v, err := range seq {
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Take returns an iterator yielding at most n items from seq.
func Take[T any](seq iter.Seq2[T, error], n int) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for v, err := range seq {
			if !yield(v, err) || err != nil {
				return
			}
			i++
			if i >= n {
				return
			}
		}
	}
}
