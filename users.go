package spotigo

import (
	"context"
	"iter"
)

// CurrentUser returns the profile of the authenticated user (GET /me).
func (c *Client) CurrentUser(ctx context.Context) (*PrivateUser, error) {
	return getOne[PrivateUser](ctx, c, "me", nil)
}

// GetUser returns a user's public profile.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	return getOne[User](ctx, c, path("users", userID), nil)
}

// TopArtists returns the user's top artists. Use Range to pick the period.
func (c *Client) TopArtists(ctx context.Context, opts ...RequestOption) (*Page[FullArtist], error) {
	return getPage[FullArtist](ctx, c, "me/top/artists", c.params(opts...))
}

// TopTracks returns the user's top tracks. Use Range to pick the period.
func (c *Client) TopTracks(ctx context.Context, opts ...RequestOption) (*Page[FullTrack], error) {
	return getPage[FullTrack](ctx, c, "me/top/tracks", c.params(opts...))
}

// FollowedArtists returns a cursor page of artists followed by the user.
func (c *Client) FollowedArtists(ctx context.Context, opts ...RequestOption) (*CursorPage[FullArtist], error) {
	q := c.params(opts...)
	q.Set("type", "artist")
	return getFieldPtr[CursorPage[FullArtist]](ctx, c, "me/following", q, "artists")
}

// AllFollowedArtists iterates over every artist followed by the user.
func (c *Client) AllFollowedArtists(ctx context.Context, opts ...RequestOption) iter.Seq2[FullArtist, error] {
	p, err := c.FollowedArtists(ctx, withMaxLimit(opts)...)
	return PaginateCursor(ctx, c, p, err)
}
