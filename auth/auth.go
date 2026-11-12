// Package auth implements Spotify's OAuth 2.0 flows: Authorization Code,
// Authorization Code with PKCE (for apps that cannot keep a secret), and
// Client Credentials. It also provides token persistence and a one-call
// loopback login helper for CLIs.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Spotify accounts service endpoints.
const (
	AuthURL  = "https://accounts.spotify.com/authorize"
	TokenURL = "https://accounts.spotify.com/api/token"
)

// Scopes. See https://developer.spotify.com/documentation/web-api/concepts/scopes
const (
	ScopeUGCImageUpload            = "ugc-image-upload"
	ScopeUserReadPlaybackState     = "user-read-playback-state"
	ScopeUserModifyPlaybackState   = "user-modify-playback-state"
	ScopeUserReadCurrentlyPlaying  = "user-read-currently-playing"
	ScopeAppRemoteControl          = "app-remote-control"
	ScopeStreaming                 = "streaming"
	ScopePlaylistReadPrivate       = "playlist-read-private"
	ScopePlaylistReadCollaborative = "playlist-read-collaborative"
	ScopePlaylistModifyPrivate     = "playlist-modify-private"
	ScopePlaylistModifyPublic      = "playlist-modify-public"
	ScopeUserFollowModify          = "user-follow-modify"
	ScopeUserFollowRead            = "user-follow-read"
	ScopeUserReadPlaybackPosition  = "user-read-playback-position"
	ScopeUserTopRead               = "user-top-read"
	ScopeUserReadRecentlyPlayed    = "user-read-recently-played"
	ScopeUserLibraryModify         = "user-library-modify"
	ScopeUserLibraryRead           = "user-library-read"
	ScopeUserReadEmail             = "user-read-email"
	ScopeUserReadPrivate           = "user-read-private"
)

// AllScopes returns every known scope (handy for prototyping, not production).
func AllScopes() []string {
	return []string{
		ScopeUGCImageUpload, ScopeUserReadPlaybackState, ScopeUserModifyPlaybackState,
		ScopeUserReadCurrentlyPlaying, ScopeAppRemoteControl, ScopeStreaming,
		ScopePlaylistReadPrivate, ScopePlaylistReadCollaborative, ScopePlaylistModifyPrivate,
		ScopePlaylistModifyPublic, ScopeUserFollowModify, ScopeUserFollowRead,
		ScopeUserReadPlaybackPosition, ScopeUserTopRead, ScopeUserReadRecentlyPlayed,
		ScopeUserLibraryModify, ScopeUserLibraryRead, ScopeUserReadEmail, ScopeUserReadPrivate,
	}
}

// Authenticator drives the user-facing OAuth flows.
type Authenticator struct {
	config *oauth2.Config
}

// Option configures an Authenticator.
type Option func(*oauth2.Config)

// WithClientID sets the client ID (default: $SPOTIFY_ID).
func WithClientID(id string) Option { return func(c *oauth2.Config) { c.ClientID = id } }

// WithClientSecret sets the client secret (default: $SPOTIFY_SECRET).
// Leave empty when using PKCE.
func WithClientSecret(s string) Option { return func(c *oauth2.Config) { c.ClientSecret = s } }

// WithRedirectURL sets the redirect URL registered in the developer dashboard.
// Spotify requires HTTPS or a loopback address such as http://127.0.0.1:8080/callback.
func WithRedirectURL(u string) Option { return func(c *oauth2.Config) { c.RedirectURL = u } }

// WithScopes sets requested scopes.
func WithScopes(scopes ...string) Option { return func(c *oauth2.Config) { c.Scopes = scopes } }

// WithEndpoint overrides the accounts endpoints (for tests).
func WithEndpoint(authURL, tokenURL string) Option {
	return func(c *oauth2.Config) {
		c.Endpoint.AuthURL, c.Endpoint.TokenURL = authURL, tokenURL
	}
}

// New creates an Authenticator. Client ID and secret default to the
// SPOTIFY_ID and SPOTIFY_SECRET environment variables.
func New(opts ...Option) *Authenticator {
	cfg := &oauth2.Config{
		ClientID:     os.Getenv("SPOTIFY_ID"),
		ClientSecret: os.Getenv("SPOTIFY_SECRET"),
		Endpoint: oauth2.Endpoint{
			AuthURL:   AuthURL,
			TokenURL:  TokenURL,
			AuthStyle: oauth2.AuthStyleAutoDetect,
		},
	}
	for _, o := range opts {
		o(cfg)
	}
	return &Authenticator{config: cfg}
}

// Config exposes the underlying oauth2 config.
func (a *Authenticator) Config() *oauth2.Config { return a.config }

// AuthOption tweaks the authorization URL.
type AuthOption = oauth2.AuthCodeOption

// ShowDialog forces the user to approve the app again.
var ShowDialog = oauth2.SetAuthURLParam("show_dialog", "true")

// AuthURL returns the URL to send the user to. state protects against CSRF
// and must be verified on callback.
func (a *Authenticator) AuthURL(state string, opts ...AuthOption) string {
	return a.config.AuthCodeURL(state, opts...)
}

// AuthURLWithPKCE returns the authorization URL for the PKCE flow along with
// the verifier that must be passed to Exchange.
func (a *Authenticator) AuthURLWithPKCE(state string, opts ...AuthOption) (authURL, verifier string) {
	verifier = oauth2.GenerateVerifier()
	opts = append(opts, oauth2.S256ChallengeOption(verifier))
	return a.config.AuthCodeURL(state, opts...), verifier
}

// Exchange trades an authorization code for a token. For PKCE pass
// Verifier(v).
func (a *Authenticator) Exchange(ctx context.Context, code string, opts ...AuthOption) (*oauth2.Token, error) {
	return a.config.Exchange(ctx, code, opts...)
}

// Verifier passes the PKCE verifier to Exchange.
func Verifier(v string) AuthOption { return oauth2.VerifierOption(v) }

// Errors returned by Callback.
var (
	ErrStateMismatch = errors.New("auth: state mismatch")
	ErrAccessDenied  = errors.New("auth: user denied access")
	ErrMissingCode   = errors.New("auth: missing code")
)

// Callback validates the redirect request (state and error parameters) and
// exchanges the code for a token.
func (a *Authenticator) Callback(ctx context.Context, state string, r *http.Request, opts ...AuthOption) (*oauth2.Token, error) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return nil, ErrAccessDenied
		}
		return nil, errors.New("auth: spotify returned error: " + e)
	}
	if q.Get("state") != state {
		return nil, ErrStateMismatch
	}
	code := q.Get("code")
	if code == "" {
		return nil, ErrMissingCode
	}
	return a.Exchange(ctx, code, opts...)
}

// TokenSource returns a refreshing token source for tok.
func (a *Authenticator) TokenSource(ctx context.Context, tok *oauth2.Token) oauth2.TokenSource {
	return a.config.TokenSource(ctx, tok)
}

// Client returns an *http.Client that authenticates requests and refreshes
// tok automatically. Use it with spotigo.New.
func (a *Authenticator) Client(ctx context.Context, tok *oauth2.Token) *http.Client {
	return oauth2.NewClient(ctx, a.TokenSource(ctx, tok))
}

// RefreshToken forces a refresh and returns the new token.
func (a *Authenticator) RefreshToken(ctx context.Context, tok *oauth2.Token) (*oauth2.Token, error) {
	t := *tok
	t.Expiry = time.Unix(1, 0) // force expiry
	t.AccessToken = ""
	return a.config.TokenSource(ctx, &t).Token()
}

// ClientCredentials returns the client-credentials config for app-only access
// (catalog data, no user data).
func ClientCredentials(clientID, clientSecret string) *clientcredentials.Config {
	if clientID == "" {
		clientID = os.Getenv("SPOTIFY_ID")
	}
	if clientSecret == "" {
		clientSecret = os.Getenv("SPOTIFY_SECRET")
	}
	return &clientcredentials.Config{ClientID: clientID, ClientSecret: clientSecret, TokenURL: TokenURL}
}

// AppClient returns an *http.Client authenticated with client credentials.
func AppClient(ctx context.Context, clientID, clientSecret string) *http.Client {
	return ClientCredentials(clientID, clientSecret).Client(ctx)
}

// RandomState returns a cryptographically random state string.
func RandomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ScopesOf returns the scopes granted in tok (from the "scope" extra field).
func ScopesOf(tok *oauth2.Token) []string {
	if tok == nil {
		return nil
	}
	s, _ := tok.Extra("scope").(string)
	return strings.Fields(s)
}

func redirectParts(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return nil, errors.New("auth: LocalLogin requires a loopback http redirect URL, e.g. http://127.0.0.1:8080/callback")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

// isLoopback reports whether host (as returned by url.URL.Hostname, i.e.
// without IPv6 brackets) is one of the loopback names Spotify accepts.
func isLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}
