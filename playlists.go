package spotigo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// MaxPlaylistCoverSize is the max size (bytes) of a JPEG cover upload.
const MaxPlaylistCoverSize = 256 * 1024

// GetPlaylist returns a playlist. Use Fields to limit the response and
// AdditionalTypes(TypeTrack, TypeEpisode) to receive podcast episodes.
func (c *Client) GetPlaylist(ctx context.Context, id ID, opts ...RequestOption) (*FullPlaylist, error) {
	return getOne[FullPlaylist](ctx, c, path("playlists", string(id)), c.paramsMarket(opts...))
}

// PlaylistItems returns a page of a playlist's items (GET /playlists/{id}/items).
func (c *Client) PlaylistItems(ctx context.Context, id ID, opts ...RequestOption) (*Page[PlaylistItem], error) {
	return getPage[PlaylistItem](ctx, c, path("playlists", string(id), "items"), c.paramsMarket(opts...))
}

// AllPlaylistItems iterates over every item of a playlist (50 per request).
func (c *Client) AllPlaylistItems(ctx context.Context, id ID, opts ...RequestOption) iter.Seq2[PlaylistItem, error] {
	p, err := c.PlaylistItems(ctx, id, withMaxLimit(opts)...)
	return Paginate(ctx, c, p, err)
}

// CurrentUserPlaylists returns a page of playlists owned or followed by the user.
func (c *Client) CurrentUserPlaylists(ctx context.Context, opts ...RequestOption) (*Page[SimplePlaylist], error) {
	return getPage[SimplePlaylist](ctx, c, "me/playlists", c.params(opts...))
}

// AllCurrentUserPlaylists iterates over every playlist of the current user.
func (c *Client) AllCurrentUserPlaylists(ctx context.Context, opts ...RequestOption) iter.Seq2[SimplePlaylist, error] {
	p, err := c.CurrentUserPlaylists(ctx, withMaxLimit(opts)...)
	return Paginate(ctx, c, p, err)
}

// UserPlaylists returns a page of another user's public playlists.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) UserPlaylists(ctx context.Context, userID string, opts ...RequestOption) (*Page[SimplePlaylist], error) {
	return getPage[SimplePlaylist](ctx, c, path("users", userID, "playlists"), c.params(opts...))
}

// PlaylistDetails describes a playlist to create or update. Nil pointer
// fields are left unchanged on update.
type PlaylistDetails struct {
	Name          string  `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Public        *bool   `json:"public,omitempty"`
	Collaborative *bool   `json:"collaborative,omitempty"`
}

// Ptr returns a pointer to v; handy for PlaylistDetails.
func Ptr[T any](v T) *T { return &v }

// CreatePlaylist creates a playlist for the current user (POST /me/playlists).
func (c *Client) CreatePlaylist(ctx context.Context, d PlaylistDetails) (*FullPlaylist, error) {
	if d.Name == "" {
		return nil, errors.New("spotigo: playlist name is required")
	}
	var p FullPlaylist
	if err := c.post(ctx, "me/playlists", nil, d, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePlaylist changes a playlist's name, description, visibility or
// collaborative flag.
func (c *Client) UpdatePlaylist(ctx context.Context, id ID, d PlaylistDetails) error {
	return c.put(ctx, path("playlists", string(id)), nil, d, nil)
}

type snapshot struct {
	SnapshotID string `json:"snapshot_id"`
}

// AddItems appends tracks/episodes to a playlist. Any number of URIs is
// accepted (chunked by 100). Returns the final snapshot ID.
func (c *Client) AddItems(ctx context.Context, id ID, uris ...URI) (string, error) {
	return c.addItems(ctx, id, -1, uris)
}

// AddItemsAt inserts items at a zero-based position, preserving their order.
func (c *Client) AddItemsAt(ctx context.Context, id ID, position int, uris ...URI) (string, error) {
	if position < 0 {
		return "", errors.New("spotigo: negative position")
	}
	return c.addItems(ctx, id, position, uris)
}

func (c *Client) addItems(ctx context.Context, id ID, pos int, uris []URI) (string, error) {
	var snap snapshot
	for _, part := range chunk(uris, playlistChunk) {
		body := map[string]any{"uris": part}
		if pos >= 0 {
			body["position"] = pos
			pos += len(part)
		}
		if err := c.post(ctx, path("playlists", string(id), "items"), nil, body, &snap); err != nil {
			return "", err
		}
	}
	return snap.SnapshotID, nil
}

// RemoveItems removes every occurrence of the given URIs from a playlist.
// snapshotID is optional ("" to target the latest version).
func (c *Client) RemoveItems(ctx context.Context, id ID, snapshotID string, uris ...URI) (string, error) {
	var snap snapshot
	for _, part := range chunk(uris, playlistChunk) {
		items := make([]map[string]URI, len(part))
		for i, u := range part {
			items[i] = map[string]URI{"uri": u}
		}
		body := map[string]any{"items": items}
		if snapshotID != "" {
			body["snapshot_id"] = snapshotID
		}
		if err := c.delete(ctx, path("playlists", string(id), "items"), nil, body, &snap); err != nil {
			return "", err
		}
		// Subsequent chunks must target the new version.
		if snapshotID != "" {
			snapshotID = snap.SnapshotID
		}
	}
	return snap.SnapshotID, nil
}

// ReplaceItems replaces the whole content of a playlist. Passing no URIs
// clears it. More than 100 URIs are handled transparently.
func (c *Client) ReplaceItems(ctx context.Context, id ID, uris ...URI) (string, error) {
	first := uris
	if len(first) > playlistChunk {
		first = uris[:playlistChunk]
	}
	if first == nil {
		first = []URI{}
	}
	var snap snapshot
	if err := c.put(ctx, path("playlists", string(id), "items"), nil, map[string]any{"uris": first}, &snap); err != nil {
		return "", err
	}
	if len(uris) > playlistChunk {
		return c.AddItems(ctx, id, uris[playlistChunk:]...)
	}
	return snap.SnapshotID, nil
}

// Reorder moves items within a playlist.
type Reorder struct {
	RangeStart   int    `json:"range_start"`
	InsertBefore int    `json:"insert_before"`
	RangeLength  int    `json:"range_length,omitempty"` // default 1
	SnapshotID   string `json:"snapshot_id,omitempty"`
}

// ReorderItems moves a range of items. Returns the new snapshot ID.
func (c *Client) ReorderItems(ctx context.Context, id ID, r Reorder) (string, error) {
	var snap snapshot
	err := c.put(ctx, path("playlists", string(id), "items"), nil, r, &snap)
	return snap.SnapshotID, err
}

// PlaylistCover returns the cover images of a playlist.
func (c *Client) PlaylistCover(ctx context.Context, id ID) (Images, error) {
	var im Images
	err := c.get(ctx, path("playlists", string(id), "images"), nil, &im)
	return im, err
}

// UploadPlaylistCover sets a custom JPEG cover (max 256 KB). Requires the
// ugc-image-upload scope.
func (c *Client) UploadPlaylistCover(ctx context.Context, id ID, jpeg io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(jpeg, MaxPlaylistCoverSize+1))
	if err != nil {
		return err
	}
	if len(data) > MaxPlaylistCoverSize {
		return fmt.Errorf("spotigo: cover image exceeds %d bytes", MaxPlaylistCoverSize)
	}
	enc := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(enc, data)
	return c.do(ctx, &request{
		method: http.MethodPut,
		path:   path("playlists", string(id), "images"),
		raw:    enc,
		ctype:  "image/jpeg",
	}, nil)
}
