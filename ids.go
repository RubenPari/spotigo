package spotigo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ID is a base-62 Spotify identifier, e.g. "6rqhFgbbKwnb9MLmUQDhG6".
type ID string

func (id ID) String() string { return string(id) }

// URI is a Spotify URI, e.g. "spotify:track:6rqhFgbbKwnb9MLmUQDhG6".
type URI string

func (u URI) String() string { return string(u) }

// ItemType is the kind of Spotify object.
type ItemType string

const (
	TypeAlbum     ItemType = "album"
	TypeArtist    ItemType = "artist"
	TypeTrack     ItemType = "track"
	TypePlaylist  ItemType = "playlist"
	TypeShow      ItemType = "show"
	TypeEpisode   ItemType = "episode"
	TypeAudiobook ItemType = "audiobook"
	TypeChapter   ItemType = "chapter"
	TypeUser      ItemType = "user"
)

// NewURI builds a URI from a type and ID.
func NewURI(t ItemType, id ID) URI { return URI("spotify:" + string(t) + ":" + string(id)) }

// ErrInvalidURI is returned by ParseURI for unrecognised input.
var ErrInvalidURI = errors.New("spotigo: invalid spotify uri or link")

// ParseURI accepts any of
//
//	spotify:track:6rqhFgbbKwnb9MLmUQDhG6
//	https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6?si=abc
//	https://open.spotify.com/intl-it/track/6rqhFgbbKwnb9MLmUQDhG6
//	spotify:user:alice:playlist:37i9dQZF1DXcBWIGoYBM5M (legacy)
//
// and returns the item type and ID.
func ParseURI(s string) (ItemType, ID, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "spotify:") {
		parts := strings.Split(s, ":")
		// Legacy user playlists: spotify:user:<u>:playlist:<id>
		if len(parts) == 5 && parts[1] == "user" && parts[3] == "playlist" {
			return TypePlaylist, ID(parts[4]), validID(parts[4])
		}
		if len(parts) != 3 {
			return "", "", fmt.Errorf("%w: %q", ErrInvalidURI, s)
		}
		return ItemType(parts[1]), ID(parts[2]), validType(parts[1], parts[2])
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || !strings.HasSuffix(u.Host, "spotify.com") {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidURI, s)
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) > 0 && strings.HasPrefix(segs[0], "intl-") {
		segs = segs[1:]
	}
	if len(segs) == 4 && segs[0] == "user" && segs[2] == "playlist" {
		segs = segs[2:]
	}
	if len(segs) != 2 {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidURI, s)
	}
	return ItemType(segs[0]), ID(segs[1]), validType(segs[0], segs[1])
}

// ParseID is like ParseURI but also accepts a bare ID, returning it as-is.
func ParseID(s string) (ID, error) {
	if !strings.Contains(s, ":") && !strings.Contains(s, "/") {
		return ID(s), validID(s)
	}
	_, id, err := ParseURI(s)
	return id, err
}

func validType(t, id string) error {
	switch ItemType(t) {
	case TypeAlbum, TypeArtist, TypeTrack, TypePlaylist, TypeShow, TypeEpisode, TypeAudiobook, TypeChapter:
		return validID(id)
	case TypeUser:
		if id == "" {
			return ErrInvalidURI
		}
		return nil
	}
	return fmt.Errorf("%w: unknown type %q", ErrInvalidURI, t)
}

func validID(id string) error {
	if id == "" || len(id) > 64 {
		return fmt.Errorf("%w: bad id %q", ErrInvalidURI, id)
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return fmt.Errorf("%w: bad id %q", ErrInvalidURI, id)
		}
	}
	return nil
}

// ID extracts the ID portion of the URI.
func (u URI) ID() ID {
	_, id, _ := ParseURI(string(u))
	return id
}

// Type extracts the type portion of the URI.
func (u URI) Type() ItemType {
	t, _, _ := ParseURI(string(u))
	return t
}

// joinCSV joins string-like values with commas.
func joinCSV[S ~string](vals []S) string {
	s := make([]string, len(vals))
	for i, v := range vals {
		s[i] = string(v)
	}
	return strings.Join(s, ",")
}

// toURIs builds URIs of type t from string-like IDs.
func toURIs[S ~string](t ItemType, ids []S) []URI {
	out := make([]URI, len(ids))
	for i, id := range ids {
		out[i] = NewURI(t, ID(id))
	}
	return out
}

func chunk[T any](s []T, size int) [][]T {
	if len(s) == 0 {
		return nil
	}
	out := make([][]T, 0, (len(s)+size-1)/size)
	for size < len(s) {
		s, out = s[size:], append(out, s[:size:size])
	}
	return append(out, s)
}
