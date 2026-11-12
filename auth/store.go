package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/oauth2"
)

// TokenStore persists OAuth tokens between runs.
type TokenStore interface {
	Load() (*oauth2.Token, error) // returns (nil, nil) if no token is stored
	Save(*oauth2.Token) error
}

// FileStore stores a token as JSON in a file with 0600 permissions.
type FileStore struct{ Path string }

// DefaultFileStore stores the token under the user config dir
// (e.g. ~/.config/spotigo/<name>.json).
func DefaultFileStore(name string) (*FileStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: filepath.Join(dir, "spotigo", name+".json")}, nil
}

// Load implements TokenStore.
func (f *FileStore) Load() (*oauth2.Token, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Save implements TokenStore. Writes are atomic.
func (f *FileStore) Save(t *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// MemoryStore is an in-memory TokenStore (useful in tests).
type MemoryStore struct {
	mu  sync.Mutex
	tok *oauth2.Token
}

// Load implements TokenStore.
func (m *MemoryStore) Load() (*oauth2.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tok, nil
}

// Save implements TokenStore.
func (m *MemoryStore) Save(t *oauth2.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tok = t
	return nil
}

// persistingSource saves tokens whenever the access token changes.
type persistingSource struct {
	mu    sync.Mutex
	src   oauth2.TokenSource
	store TokenStore
	last  string
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	t, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.AccessToken != p.last {
		p.last = t.AccessToken
		if err := p.store.Save(t); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// PersistentClient returns an authenticated *http.Client whose refreshed
// tokens are written back to store, so a refresh token rotation by Spotify
// is never lost.
func (a *Authenticator) PersistentClient(ctx context.Context, tok *oauth2.Token, store TokenStore) *http.Client {
	ps := &persistingSource{src: a.TokenSource(ctx, tok), store: store, last: tok.AccessToken}
	return oauth2.NewClient(ctx, oauth2.ReuseTokenSource(nil, ps))
}
