package spotigo

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RequestOption customises query parameters of a single request.
type RequestOption func(url.Values)

// Limit sets the maximum number of items to return.
func Limit(n int) RequestOption { return func(v url.Values) { v.Set("limit", strconv.Itoa(n)) } }

// Offset sets the index of the first item to return.
func Offset(n int) RequestOption { return func(v url.Values) { v.Set("offset", strconv.Itoa(n)) } }

// Market sets an ISO 3166-1 alpha-2 country code (or "from_token") enabling
// track relinking. Overrides the client default set by WithMarket.
func Market(code string) RequestOption { return func(v url.Values) { v.Set("market", code) } }

// Country sets the country for browse endpoints.
func Country(code string) RequestOption { return func(v url.Values) { v.Set("country", code) } }

// Locale sets the locale (e.g. "it_IT") for browse endpoints.
func Locale(l string) RequestOption { return func(v url.Values) { v.Set("locale", l) } }

// Fields filters playlist responses, e.g. "items(track(name,uri))".
func Fields(f string) RequestOption { return func(v url.Values) { v.Set("fields", f) } }

// After sets the cursor for cursor-paged endpoints.
func After(cursor string) RequestOption { return func(v url.Values) { v.Set("after", cursor) } }

// Before sets the "before" cursor (recently played).
func Before(cursor string) RequestOption { return func(v url.Values) { v.Set("before", cursor) } }

// AfterTime / BeforeTime are typed helpers for recently-played cursors.
func AfterTime(t time.Time) RequestOption {
	return After(strconv.FormatInt(t.UnixMilli(), 10))
}

// BeforeTime sets the "before" cursor from a time.
func BeforeTime(t time.Time) RequestOption {
	return Before(strconv.FormatInt(t.UnixMilli(), 10))
}

// TimeRange is used by the top items endpoints.
type TimeRange string

const (
	ShortTerm  TimeRange = "short_term"  // ~4 weeks
	MediumTerm TimeRange = "medium_term" // ~6 months
	LongTerm   TimeRange = "long_term"   // ~1 year
)

// Range sets the time range for top artists/tracks.
func Range(r TimeRange) RequestOption { return func(v url.Values) { v.Set("time_range", string(r)) } }

// AdditionalTypes asks for episodes as well as tracks in playlists/player.
func AdditionalTypes(types ...ItemType) RequestOption {
	return func(v url.Values) { v.Set("additional_types", joinCSV(types)) }
}

// IncludeGroups filters artist albums: album, single, appears_on, compilation.
func IncludeGroups(groups ...string) RequestOption {
	return func(v url.Values) { v.Set("include_groups", strings.Join(groups, ",")) }
}

// DeviceID targets a specific playback device.
func DeviceID(id string) RequestOption { return func(v url.Values) { v.Set("device_id", id) } }

// IncludeExternalAudio makes search return externally hosted audio content.
func IncludeExternalAudio() RequestOption {
	return func(v url.Values) { v.Set("include_external", "audio") }
}

// Param sets an arbitrary query parameter (escape hatch).
func Param(key, value string) RequestOption { return func(v url.Values) { v.Set(key, value) } }

func (c *Client) params(opts ...RequestOption) url.Values {
	v := url.Values{}
	for _, o := range opts {
		o(v)
	}
	return v
}

// paramsMarket is like params but applies the client's default market.
func (c *Client) paramsMarket(opts ...RequestOption) url.Values {
	v := c.params(opts...)
	if c.market != "" && v.Get("market") == "" {
		v.Set("market", c.market)
	}
	return v
}
