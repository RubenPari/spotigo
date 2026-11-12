package spotigo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
)

// Request sizing limits imposed by the Spotify Web API.
const (
	defaultConcurrency = 8   // parallel requests when fanning out
	maxPageLimit       = 50  // max "limit" accepted by paged endpoints
	albumBatch         = 20  // max IDs per GET /albums
	catalogBatch       = 50  // max IDs per GET /tracks, /artists, ...
	followChunk        = 50  // max IDs per /me/following write
	libraryChunk       = 40  // max URIs per /me/library request
	playlistChunk      = 100 // max items per playlist write
)

// BatchMode controls how multi-ID lookups (GetTracks, GetAlbums, ...) are
// performed.
//
// In February 2026 Spotify removed the batch endpoints (GET /tracks?ids=...,
// GET /albums?ids=..., ...) for Development Mode apps, while Extended Quota
// apps keep them. spotigo hides the difference.
type BatchMode int

const (
	// BatchAuto uses the batch endpoint and permanently switches this client
	// to concurrent single-item requests the first time the batch endpoint
	// is rejected (403/404/410). This is the default.
	BatchAuto BatchMode = iota
	// BatchEndpoint always uses the batch endpoints (Extended Quota apps).
	BatchEndpoint
	// BatchFanOut always issues concurrent single-item requests.
	BatchFanOut
)

// WithBatchMode sets the BatchMode (default BatchAuto).
func WithBatchMode(m BatchMode) Option { return func(c *Client) { c.batchMode = m } }

// WithConcurrency sets how many requests may run in parallel when fanning
// out single-item requests (default 8).
func WithConcurrency(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.concurrency = n
		}
	}
}

func batchRejected(err error) bool {
	e, ok := AsError(err)
	return ok && (e.Status == http.StatusForbidden || e.Status == http.StatusNotFound || e.Status == http.StatusGone)
}

// getMany fetches objects of the given plural kind ("tracks") by ID,
// preserving order. Unknown IDs yield nil entries, matching the batch API.
func getMany[T any](ctx context.Context, c *Client, kind string, ids []ID, max int, q url.Values) ([]*T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if c.batchMode != BatchFanOut && !c.noBatch.Load() {
		out, rejected, err := batchGet[T](ctx, c, kind, ids, max, q)
		if !rejected || c.batchMode != BatchAuto {
			return out, err
		}
		c.debug("batch endpoint rejected, switching to fan-out", "kind", kind, "err", err)
		c.noBatch.Store(true)
	}
	return fanOutGet[T](ctx, c, kind, ids, q)
}

// batchGet uses the multi-ID endpoint (GET /{kind}?ids=...), max IDs per
// call. rejected reports that the very first request was refused in a way
// meaning the endpoint is unavailable to this app (see batchRejected).
func batchGet[T any](ctx context.Context, c *Client, kind string, ids []ID, max int, q url.Values) (out []*T, rejected bool, err error) {
	out = make([]*T, 0, len(ids))
	for i, part := range chunk(ids, max) {
		qq := cloneValues(q)
		qq.Set("ids", joinCSV(part))
		var raw map[string][]*T
		if err := c.get(ctx, kind, qq, &raw); err != nil {
			return nil, i == 0 && batchRejected(err), err
		}
		out = append(out, raw[kind]...)
	}
	return out, false, nil
}

// fanOutGet issues one GET /{kind}/{id} per ID, concurrently.
func fanOutGet[T any](ctx context.Context, c *Client, kind string, ids []ID, q url.Values) ([]*T, error) {
	return fanOut(ctx, c, ids, func(ctx context.Context, id ID) (*T, error) {
		var v T
		if err := c.get(ctx, kind+"/"+url.PathEscape(string(id)), q, &v); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrBadRequest) {
				return nil, nil
			}
			return nil, err
		}
		return &v, nil
	})
}

// fanOut runs fn for every id with bounded concurrency, preserving order and
// cancelling outstanding work on the first error.
func fanOut[T any](ctx context.Context, c *Client, ids []ID, fn func(context.Context, ID) (*T, error)) ([]*T, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	n := c.concurrency
	if n <= 0 {
		n = defaultConcurrency
	}
	out := make([]*T, len(ids))
	sem := make(chan struct{}, n)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for i, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			v, err := fn(ctx, id)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			out[i] = v
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+1)
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}
