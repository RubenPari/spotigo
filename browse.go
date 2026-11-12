package spotigo

import "context"

// Category is a browse category.
type Category struct {
	Href  string `json:"href"`
	Icons Images `json:"icons"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

// NewReleases returns new album releases.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) NewReleases(ctx context.Context, opts ...RequestOption) (*Page[SimpleAlbum], error) {
	return getFieldPtr[Page[SimpleAlbum]](ctx, c, "browse/new-releases", c.params(opts...), "albums")
}

// Categories returns browse categories.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) Categories(ctx context.Context, opts ...RequestOption) (*Page[Category], error) {
	return getFieldPtr[Page[Category]](ctx, c, "browse/categories", c.params(opts...), "categories")
}

// GetCategory returns a single browse category.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) GetCategory(ctx context.Context, id string, opts ...RequestOption) (*Category, error) {
	return getOne[Category](ctx, c, path("browse/categories", id), c.params(opts...))
}

// Markets returns the markets where Spotify is available.
//
// Deprecated: removed for Development Mode apps in February 2026.
func (c *Client) Markets(ctx context.Context) ([]string, error) {
	return getField[[]string](ctx, c, "markets", nil, "markets")
}
