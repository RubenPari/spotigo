package spotigo

import (
	"encoding/json"
	"strings"
	"time"
)

// Note on 2026 API changes: since February 2026 Spotify no longer returns
// several fields (popularity, available_markets, label, followers, ...) to
// Development Mode apps. Those fields are kept here because Extended Quota
// apps still receive them; they are simply left at their zero value otherwise.

// ExternalURLs maps a provider ("spotify") to a URL.
type ExternalURLs map[string]string

// Spotify returns the open.spotify.com link, if present.
func (e ExternalURLs) Spotify() string { return e["spotify"] }

// ExternalIDs maps an id kind ("isrc", "ean", "upc") to its value.
type ExternalIDs map[string]string

// Image is an artwork image. Width/Height may be zero when unknown.
type Image struct {
	URL    string `json:"url"`
	Height int    `json:"height"`
	Width  int    `json:"width"`
}

// Images is a list of images, typically ordered widest first.
type Images []Image

// Largest returns the widest image (or zero Image if empty).
func (im Images) Largest() Image {
	var best Image
	for _, i := range im {
		if i.Width >= best.Width || best.URL == "" {
			best = i
		}
	}
	return best
}

// Smallest returns the narrowest image wider than minWidth, falling back to the largest.
func (im Images) Smallest(minWidth int) Image {
	var best Image
	for _, i := range im {
		if i.Width >= minWidth && (best.URL == "" || i.Width < best.Width) {
			best = i
		}
	}
	if best.URL == "" {
		return im.Largest()
	}
	return best
}

// Followers holds follower information.
type Followers struct {
	Href  string `json:"href"`
	Total int    `json:"total"`
}

// Copyright statement.
type Copyright struct {
	Text string `json:"text"`
	Type string `json:"type"` // "C" or "P"
}

// Restrictions explains why content is unavailable.
type Restrictions struct {
	Reason string `json:"reason"` // market, product, explicit
}

// ResumePoint describes the user's progress in an episode/chapter.
type ResumePoint struct {
	FullyPlayed      bool `json:"fully_played"`
	ResumePositionMs int  `json:"resume_position_ms"`
}

// ReleaseDate is a date with variable precision ("2021", "2021-03", "2021-03-14").
type ReleaseDate string

// Time parses the release date according to its precision. Missing month/day
// default to January / the 1st.
func (d ReleaseDate) Time() (time.Time, error) {
	s := string(d)
	switch strings.Count(s, "-") {
	case 0:
		return time.Parse("2006", s)
	case 1:
		return time.Parse("2006-01", s)
	default:
		return time.Parse(time.DateOnly, s)
	}
}

// Year returns the release year (0 if unparsable).
func (d ReleaseDate) Year() int {
	t, err := d.Time()
	if err != nil {
		return 0
	}
	return t.Year()
}

// ---- Artists ----

// SimpleArtist is the simplified artist object.
type SimpleArtist struct {
	ExternalURLs ExternalURLs `json:"external_urls"`
	Href         string       `json:"href"`
	ID           ID           `json:"id"`
	Name         string       `json:"name"`
	Type         ItemType     `json:"type"`
	URI          URI          `json:"uri"`
}

// FullArtist is the full artist object.
type FullArtist struct {
	SimpleArtist
	Followers  Followers `json:"followers"`
	Genres     []string  `json:"genres"`
	Images     Images    `json:"images"`
	Popularity int       `json:"popularity"`
}

// ArtistNames joins the names of artists with ", ".
func ArtistNames(as []SimpleArtist) string {
	n := make([]string, len(as))
	for i, a := range as {
		n[i] = a.Name
	}
	return strings.Join(n, ", ")
}

// ---- Albums ----

// SimpleAlbum is the simplified album object.
type SimpleAlbum struct {
	AlbumType            string         `json:"album_type"`
	AlbumGroup           string         `json:"album_group,omitempty"`
	TotalTracks          int            `json:"total_tracks"`
	AvailableMarkets     []string       `json:"available_markets,omitempty"`
	ExternalURLs         ExternalURLs   `json:"external_urls"`
	Href                 string         `json:"href"`
	ID                   ID             `json:"id"`
	Images               Images         `json:"images"`
	Name                 string         `json:"name"`
	ReleaseDate          ReleaseDate    `json:"release_date"`
	ReleaseDatePrecision string         `json:"release_date_precision"`
	Restrictions         *Restrictions  `json:"restrictions,omitempty"`
	Type                 ItemType       `json:"type"`
	URI                  URI            `json:"uri"`
	Artists              []SimpleArtist `json:"artists"`
}

// FullAlbum is the full album object.
type FullAlbum struct {
	SimpleAlbum
	Tracks      Page[SimpleTrack] `json:"tracks"`
	Copyrights  []Copyright       `json:"copyrights"`
	ExternalIDs ExternalIDs       `json:"external_ids"`
	Genres      []string          `json:"genres"`
	Label       string            `json:"label"`
	Popularity  int               `json:"popularity"`
}

// SavedAlbum is an album in the user's library.
type SavedAlbum struct {
	AddedAt time.Time `json:"added_at"`
	Album   FullAlbum `json:"album"`
}

// ---- Tracks ----

// LinkedTrack identifies the original track when relinking occurred.
type LinkedTrack struct {
	ExternalURLs ExternalURLs `json:"external_urls"`
	Href         string       `json:"href"`
	ID           ID           `json:"id"`
	Type         ItemType     `json:"type"`
	URI          URI          `json:"uri"`
}

// SimpleTrack is the simplified track object.
type SimpleTrack struct {
	Artists          []SimpleArtist `json:"artists"`
	AvailableMarkets []string       `json:"available_markets,omitempty"`
	DiscNumber       int            `json:"disc_number"`
	DurationMs       int            `json:"duration_ms"`
	Explicit         bool           `json:"explicit"`
	ExternalURLs     ExternalURLs   `json:"external_urls"`
	Href             string         `json:"href"`
	ID               ID             `json:"id"`
	IsPlayable       *bool          `json:"is_playable,omitempty"`
	LinkedFrom       *LinkedTrack   `json:"linked_from,omitempty"`
	Restrictions     *Restrictions  `json:"restrictions,omitempty"`
	Name             string         `json:"name"`
	PreviewURL       string         `json:"preview_url"`
	TrackNumber      int            `json:"track_number"`
	Type             ItemType       `json:"type"`
	URI              URI            `json:"uri"`
	IsLocal          bool           `json:"is_local"`
}

// Duration returns the track length.
func (t SimpleTrack) Duration() time.Duration { return time.Duration(t.DurationMs) * time.Millisecond }

// FullTrack is the full track object.
type FullTrack struct {
	SimpleTrack
	Album       SimpleAlbum `json:"album"`
	ExternalIDs ExternalIDs `json:"external_ids"`
	Popularity  int         `json:"popularity"`
}

// ISRC returns the track's ISRC code, if known.
func (t FullTrack) ISRC() string { return t.ExternalIDs["isrc"] }

// SavedTrack is a track in the user's library.
type SavedTrack struct {
	AddedAt time.Time `json:"added_at"`
	Track   FullTrack `json:"track"`
}

// ---- Shows & episodes ----

// SimpleShow is the simplified show object.
type SimpleShow struct {
	AvailableMarkets   []string     `json:"available_markets,omitempty"`
	Copyrights         []Copyright  `json:"copyrights"`
	Description        string       `json:"description"`
	HTMLDescription    string       `json:"html_description"`
	Explicit           bool         `json:"explicit"`
	ExternalURLs       ExternalURLs `json:"external_urls"`
	Href               string       `json:"href"`
	ID                 ID           `json:"id"`
	Images             Images       `json:"images"`
	IsExternallyHosted bool         `json:"is_externally_hosted"`
	Languages          []string     `json:"languages"`
	MediaType          string       `json:"media_type"`
	Name               string       `json:"name"`
	Publisher          string       `json:"publisher,omitempty"`
	Type               ItemType     `json:"type"`
	URI                URI          `json:"uri"`
	TotalEpisodes      int          `json:"total_episodes"`
}

// FullShow is the full show object.
type FullShow struct {
	SimpleShow
	Episodes Page[SimpleEpisode] `json:"episodes"`
}

// SavedShow is a show in the user's library.
type SavedShow struct {
	AddedAt time.Time  `json:"added_at"`
	Show    SimpleShow `json:"show"`
}

// SimpleEpisode is the simplified episode object.
type SimpleEpisode struct {
	AudioPreviewURL      string        `json:"audio_preview_url"`
	Description          string        `json:"description"`
	HTMLDescription      string        `json:"html_description"`
	DurationMs           int           `json:"duration_ms"`
	Explicit             bool          `json:"explicit"`
	ExternalURLs         ExternalURLs  `json:"external_urls"`
	Href                 string        `json:"href"`
	ID                   ID            `json:"id"`
	Images               Images        `json:"images"`
	IsExternallyHosted   bool          `json:"is_externally_hosted"`
	IsPlayable           bool          `json:"is_playable"`
	Languages            []string      `json:"languages"`
	Name                 string        `json:"name"`
	ReleaseDate          ReleaseDate   `json:"release_date"`
	ReleaseDatePrecision string        `json:"release_date_precision"`
	ResumePoint          *ResumePoint  `json:"resume_point,omitempty"`
	Restrictions         *Restrictions `json:"restrictions,omitempty"`
	Type                 ItemType      `json:"type"`
	URI                  URI           `json:"uri"`
}

// Duration returns the episode length.
func (e SimpleEpisode) Duration() time.Duration {
	return time.Duration(e.DurationMs) * time.Millisecond
}

// FullEpisode is the full episode object.
type FullEpisode struct {
	SimpleEpisode
	Show SimpleShow `json:"show"`
}

// SavedEpisode is an episode in the user's library.
type SavedEpisode struct {
	AddedAt time.Time   `json:"added_at"`
	Episode FullEpisode `json:"episode"`
}

// ---- Audiobooks & chapters ----

// Person is an author or narrator.
type Person struct {
	Name string `json:"name"`
}

// SimpleAudiobook is the simplified audiobook object.
type SimpleAudiobook struct {
	Authors          []Person     `json:"authors"`
	AvailableMarkets []string     `json:"available_markets,omitempty"`
	Copyrights       []Copyright  `json:"copyrights"`
	Description      string       `json:"description"`
	HTMLDescription  string       `json:"html_description"`
	Edition          string       `json:"edition"`
	Explicit         bool         `json:"explicit"`
	ExternalURLs     ExternalURLs `json:"external_urls"`
	Href             string       `json:"href"`
	ID               ID           `json:"id"`
	Images           Images       `json:"images"`
	Languages        []string     `json:"languages"`
	MediaType        string       `json:"media_type"`
	Name             string       `json:"name"`
	Narrators        []Person     `json:"narrators"`
	Publisher        string       `json:"publisher,omitempty"`
	Type             ItemType     `json:"type"`
	URI              URI          `json:"uri"`
	TotalChapters    int          `json:"total_chapters"`
}

// FullAudiobook is the full audiobook object.
type FullAudiobook struct {
	SimpleAudiobook
	Chapters Page[SimpleChapter] `json:"chapters"`
}

// SimpleChapter is the simplified chapter object.
type SimpleChapter struct {
	AudioPreviewURL      string        `json:"audio_preview_url"`
	AvailableMarkets     []string      `json:"available_markets,omitempty"`
	ChapterNumber        int           `json:"chapter_number"`
	Description          string        `json:"description"`
	HTMLDescription      string        `json:"html_description"`
	DurationMs           int           `json:"duration_ms"`
	Explicit             bool          `json:"explicit"`
	ExternalURLs         ExternalURLs  `json:"external_urls"`
	Href                 string        `json:"href"`
	ID                   ID            `json:"id"`
	Images               Images        `json:"images"`
	IsPlayable           bool          `json:"is_playable"`
	Languages            []string      `json:"languages"`
	Name                 string        `json:"name"`
	ReleaseDate          ReleaseDate   `json:"release_date"`
	ReleaseDatePrecision string        `json:"release_date_precision"`
	ResumePoint          *ResumePoint  `json:"resume_point,omitempty"`
	Restrictions         *Restrictions `json:"restrictions,omitempty"`
	Type                 ItemType      `json:"type"`
	URI                  URI           `json:"uri"`
}

// FullChapter is the full chapter object.
type FullChapter struct {
	SimpleChapter
	Audiobook SimpleAudiobook `json:"audiobook"`
}

// ---- Users ----

// User is a public user profile.
type User struct {
	DisplayName  string       `json:"display_name"`
	ExternalURLs ExternalURLs `json:"external_urls"`
	Followers    Followers    `json:"followers"`
	Href         string       `json:"href"`
	ID           string       `json:"id"`
	Images       Images       `json:"images"`
	Type         ItemType     `json:"type"`
	URI          URI          `json:"uri"`
}

// ExplicitContent holds the user's explicit content settings.
type ExplicitContent struct {
	FilterEnabled bool `json:"filter_enabled"`
	FilterLocked  bool `json:"filter_locked"`
}

// PrivateUser is the current user's profile (GET /me).
type PrivateUser struct {
	User
	// AccountID is a stable account identifier (added May 2026); prefer it
	// over ID when linking accounts.
	AccountID       string           `json:"account_id,omitempty"`
	Country         string           `json:"country,omitempty"`
	Email           string           `json:"email,omitempty"`
	ExplicitContent *ExplicitContent `json:"explicit_content,omitempty"`
	Product         string           `json:"product,omitempty"`
}

// ---- Playlists ----

// PlaylistItemsRef is the {href,total} summary of a playlist's items.
type PlaylistItemsRef struct {
	Href  string `json:"href"`
	Total int    `json:"total"`
}

// SimplePlaylist is the simplified playlist object.
type SimplePlaylist struct {
	Collaborative bool         `json:"collaborative"`
	Description   string       `json:"description"`
	ExternalURLs  ExternalURLs `json:"external_urls"`
	Href          string       `json:"href"`
	ID            ID           `json:"id"`
	Images        Images       `json:"images"`
	Name          string       `json:"name"`
	Owner         User         `json:"owner"`
	Public        *bool        `json:"public"`
	SnapshotID    string       `json:"snapshot_id"`
	// Items summarises the playlist content. Decoded from "items" (2026 API)
	// or the legacy "tracks" field.
	Items PlaylistItemsRef `json:"-"`
	Type  ItemType         `json:"type"`
	URI   URI              `json:"uri"`
}

// UnmarshalJSON accepts both the 2026 "items" and legacy "tracks" fields.
func (p *SimplePlaylist) UnmarshalJSON(b []byte) error {
	type plain SimplePlaylist
	var aux struct {
		plain
		Items  *PlaylistItemsRef `json:"items"`
		Tracks *PlaylistItemsRef `json:"tracks"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*p = SimplePlaylist(aux.plain)
	if aux.Items != nil {
		p.Items = *aux.Items
	} else if aux.Tracks != nil {
		p.Items = *aux.Tracks
	}
	return nil
}

// FullPlaylist is the full playlist object.
type FullPlaylist struct {
	SimplePlaylist
	Followers Followers
	// Content is the first page of items (from "items" or legacy "tracks").
	// Note: since 2026 Spotify only returns it for playlists the user owns
	// or collaborates on.
	Content Page[PlaylistItem]
}

// UnmarshalJSON accepts both the 2026 "items" and legacy "tracks" fields.
func (p *FullPlaylist) UnmarshalJSON(b []byte) error {
	// plain drops SimplePlaylist's UnmarshalJSON so everything is decoded in
	// a single pass; its Items field is tagged "-" and does not clash.
	type plain SimplePlaylist
	var aux struct {
		plain
		Followers Followers           `json:"followers"`
		Items     *Page[PlaylistItem] `json:"items"`
		Tracks    *Page[PlaylistItem] `json:"tracks"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*p = FullPlaylist{SimplePlaylist: SimplePlaylist(aux.plain), Followers: aux.Followers}
	switch {
	case aux.Items != nil:
		p.Content = *aux.Items
	case aux.Tracks != nil:
		p.Content = *aux.Tracks
	}
	p.SimplePlaylist.Items = PlaylistItemsRef{Href: p.Content.Href, Total: p.Content.Total}
	return nil
}

// PlaylistItem is an entry of a playlist: a track or an episode.
type PlaylistItem struct {
	AddedAt time.Time `json:"added_at"`
	AddedBy *User     `json:"added_by"`
	IsLocal bool      `json:"is_local"`
	// Item is the playable content (decoded from "item", or legacy "track").
	Item Playable `json:"-"`
}

// UnmarshalJSON accepts both "item" (2026) and legacy "track" fields.
func (pi *PlaylistItem) UnmarshalJSON(b []byte) error {
	type plain PlaylistItem
	var aux struct {
		plain
		Item  json.RawMessage `json:"item"`
		Track json.RawMessage `json:"track"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*pi = PlaylistItem(aux.plain)
	raw := aux.Item
	if len(raw) == 0 || string(raw) == "null" {
		raw = aux.Track
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, &pi.Item)
}

// Playable is a track or an episode (the union returned by playlists, the
// player and the queue). Exactly one of Track and Episode is non-nil.
type Playable struct {
	Track   *FullTrack
	Episode *FullEpisode
}

// UnmarshalJSON dispatches on the "type" field.
func (p *Playable) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var probe struct {
		Type ItemType `json:"type"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	if probe.Type == TypeEpisode {
		p.Episode = new(FullEpisode)
		return json.Unmarshal(b, p.Episode)
	}
	p.Track = new(FullTrack)
	return json.Unmarshal(b, p.Track)
}

// MarshalJSON encodes whichever value is set.
func (p Playable) MarshalJSON() ([]byte, error) {
	switch {
	case p.Track != nil:
		return json.Marshal(p.Track)
	case p.Episode != nil:
		return json.Marshal(p.Episode)
	}
	return []byte("null"), nil
}

// Name returns the name of the track or episode.
func (p Playable) Name() string {
	switch {
	case p.Track != nil:
		return p.Track.Name
	case p.Episode != nil:
		return p.Episode.Name
	}
	return ""
}

// URI returns the URI of the track or episode.
func (p Playable) URI() URI {
	switch {
	case p.Track != nil:
		return p.Track.URI
	case p.Episode != nil:
		return p.Episode.URI
	}
	return ""
}

// Duration returns the length of the track or episode.
func (p Playable) Duration() time.Duration {
	switch {
	case p.Track != nil:
		return p.Track.Duration()
	case p.Episode != nil:
		return p.Episode.Duration()
	}
	return 0
}

// IsZero reports whether neither a track nor an episode is set.
func (p Playable) IsZero() bool { return p.Track == nil && p.Episode == nil }
