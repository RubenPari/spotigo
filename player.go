package spotigo

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Device is a Spotify Connect device.
type Device struct {
	ID               string `json:"id"`
	IsActive         bool   `json:"is_active"`
	IsPrivateSession bool   `json:"is_private_session"`
	IsRestricted     bool   `json:"is_restricted"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	VolumePercent    *int   `json:"volume_percent"`
	SupportsVolume   bool   `json:"supports_volume"`
}

// PlaybackContext describes what the user is playing from (album, playlist...).
type PlaybackContext struct {
	Type         ItemType     `json:"type"`
	Href         string       `json:"href"`
	ExternalURLs ExternalURLs `json:"external_urls"`
	URI          URI          `json:"uri"`
}

// RepeatMode is the player's repeat state.
type RepeatMode string

const (
	RepeatOff     RepeatMode = "off"
	RepeatTrack   RepeatMode = "track"
	RepeatContext RepeatMode = "context"
)

// CurrentlyPlaying is the response of GET /me/player/currently-playing.
type CurrentlyPlaying struct {
	Context              *PlaybackContext `json:"context"`
	Timestamp            int64            `json:"timestamp"`
	ProgressMs           int              `json:"progress_ms"`
	IsPlaying            bool             `json:"is_playing"`
	Item                 Playable         `json:"item"`
	CurrentlyPlayingType string           `json:"currently_playing_type"`
	Actions              struct {
		Disallows map[string]bool `json:"disallows"`
	} `json:"actions"`
}

// Progress returns the playback position.
func (c CurrentlyPlaying) Progress() time.Duration {
	return time.Duration(c.ProgressMs) * time.Millisecond
}

// PlaybackState is the full player state (GET /me/player).
type PlaybackState struct {
	CurrentlyPlaying
	Device       Device     `json:"device"`
	RepeatState  RepeatMode `json:"repeat_state"`
	ShuffleState bool       `json:"shuffle_state"`
}

// PlayHistory is an entry of the recently played list.
type PlayHistory struct {
	Track    FullTrack        `json:"track"`
	PlayedAt time.Time        `json:"played_at"`
	Context  *PlaybackContext `json:"context"`
}

// Queue is the user's playback queue.
type Queue struct {
	CurrentlyPlaying Playable   `json:"currently_playing"`
	Queue            []Playable `json:"queue"`
}

// getOptional fetches p and returns nil (without error) when Spotify answers
// with 204 No Content or an empty body, as the player endpoints do when
// nothing is playing.
func getOptional[T any](ctx context.Context, c *Client, p string, q url.Values) (*T, error) {
	var v *T
	if err := c.get(ctx, p, q, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// PlaybackState returns the current playback state, or (nil, nil) if
// nothing is playing.
func (c *Client) PlaybackState(ctx context.Context, opts ...RequestOption) (*PlaybackState, error) {
	return getOptional[PlaybackState](ctx, c, "me/player", c.paramsMarket(opts...))
}

// CurrentlyPlaying returns the item being played, or (nil, nil) if nothing is.
func (c *Client) CurrentlyPlaying(ctx context.Context, opts ...RequestOption) (*CurrentlyPlaying, error) {
	return getOptional[CurrentlyPlaying](ctx, c, "me/player/currently-playing", c.paramsMarket(opts...))
}

// Devices lists the user's available Spotify Connect devices.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	return getField[[]Device](ctx, c, "me/player/devices", nil, "devices")
}

// TransferPlayback moves playback to a device. If play is true playback
// starts on the new device.
func (c *Client) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	body := map[string]any{"device_ids": []string{deviceID}, "play": play}
	return c.put(ctx, "me/player", nil, body, nil)
}

// PlayOptions configures Play. All fields are optional; with no fields set
// Play resumes playback.
type PlayOptions struct {
	DeviceID string `json:"-"`
	// ContextURI plays an album, artist, playlist or show.
	ContextURI URI `json:"context_uri,omitempty"`
	// URIs plays a list of tracks/episodes.
	URIs []URI `json:"uris,omitempty"`
	// Offset within the context: either a zero-based position or a URI.
	Offset *PlayOffset `json:"offset,omitempty"`
	// Position is the start position within the first item.
	Position time.Duration `json:"-"`
}

// PlayOffset selects where to start within a context.
type PlayOffset struct {
	Position *int `json:"position,omitempty"`
	URI      URI  `json:"uri,omitempty"`
}

// AtPosition is a helper building a PlayOffset from a position.
func AtPosition(n int) *PlayOffset { return &PlayOffset{Position: &n} }

// AtURI is a helper building a PlayOffset from a URI.
func AtURI(u URI) *PlayOffset { return &PlayOffset{URI: u} }

// Play starts or resumes playback. Requires Spotify Premium.
func (c *Client) Play(ctx context.Context, o *PlayOptions) error {
	if o == nil {
		o = &PlayOptions{}
	}
	// With nothing set, send no body at all: that resumes playback.
	var body any
	if o.ContextURI != "" || len(o.URIs) > 0 || o.Offset != nil || o.Position > 0 {
		b := struct {
			*PlayOptions
			PositionMs int64 `json:"position_ms,omitempty"`
		}{PlayOptions: o}
		if o.Position > 0 {
			b.PositionMs = o.Position.Milliseconds()
		}
		body = b
	}
	return c.put(ctx, "me/player/play", deviceQuery(o.DeviceID), body, nil)
}

func deviceQuery(id string, kv ...string) url.Values {
	q := url.Values{}
	if id != "" {
		q.Set("device_id", id)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	return q
}

// Pause pauses playback. deviceID may be empty for the active device.
func (c *Client) Pause(ctx context.Context, deviceID string) error {
	return c.put(ctx, "me/player/pause", deviceQuery(deviceID), nil, nil)
}

// Next skips to the next item.
func (c *Client) Next(ctx context.Context, deviceID string) error {
	return c.post(ctx, "me/player/next", deviceQuery(deviceID), nil, nil)
}

// Previous skips to the previous item.
func (c *Client) Previous(ctx context.Context, deviceID string) error {
	return c.post(ctx, "me/player/previous", deviceQuery(deviceID), nil, nil)
}

// Seek moves to a position in the current item.
func (c *Client) Seek(ctx context.Context, pos time.Duration, deviceID string) error {
	return c.put(ctx, "me/player/seek", deviceQuery(deviceID, "position_ms", strconv.FormatInt(pos.Milliseconds(), 10)), nil, nil)
}

// SetRepeat sets the repeat mode.
func (c *Client) SetRepeat(ctx context.Context, mode RepeatMode, deviceID string) error {
	return c.put(ctx, "me/player/repeat", deviceQuery(deviceID, "state", string(mode)), nil, nil)
}

// SetVolume sets the volume (0-100).
func (c *Client) SetVolume(ctx context.Context, percent int, deviceID string) error {
	percent = max(0, min(100, percent))
	return c.put(ctx, "me/player/volume", deviceQuery(deviceID, "volume_percent", strconv.Itoa(percent)), nil, nil)
}

// SetShuffle toggles shuffle.
func (c *Client) SetShuffle(ctx context.Context, on bool, deviceID string) error {
	return c.put(ctx, "me/player/shuffle", deviceQuery(deviceID, "state", strconv.FormatBool(on)), nil, nil)
}

// Queue returns the user's playback queue.
func (c *Client) Queue(ctx context.Context) (*Queue, error) {
	return getOne[Queue](ctx, c, "me/player/queue", nil)
}

// AddToQueue appends a track or episode to the queue.
func (c *Client) AddToQueue(ctx context.Context, uri URI, deviceID string) error {
	return c.post(ctx, "me/player/queue", deviceQuery(deviceID, "uri", string(uri)), nil, nil)
}

// RecentlyPlayed returns recently played tracks. Use AfterTime/BeforeTime
// and Limit (max 50) to page.
func (c *Client) RecentlyPlayed(ctx context.Context, opts ...RequestOption) (*CursorPage[PlayHistory], error) {
	return getOne[CursorPage[PlayHistory]](ctx, c, "me/player/recently-played", c.params(opts...))
}
