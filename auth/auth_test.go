package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func fakeTokenServer(t *testing.T, wantVerifier *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "authorization_code" {
			if r.Form.Get("code") != "good-code" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			if wantVerifier != nil && r.Form.Get("code_verifier") == "" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"missing verifier"}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + r.Form.Get("grant_type"), "token_type": "Bearer",
			"expires_in": 3600, "refresh_token": "refresh", "scope": "user-read-private user-top-read",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestAuthURL(t *testing.T) {
	a := New(WithClientID("cid"), WithRedirectURL("http://127.0.0.1:9/cb"), WithScopes(ScopeUserTopRead))
	u, verifier := a.AuthURLWithPKCE("st", ShowDialog)
	p, _ := url.Parse(u)
	q := p.Query()
	if p.Host != "accounts.spotify.com" || q.Get("client_id") != "cid" || q.Get("state") != "st" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("show_dialog") != "true" ||
		q.Get("scope") != ScopeUserTopRead || verifier == "" {
		t.Fatalf("bad url %s", u)
	}
}

func TestCallback(t *testing.T) {
	ts := fakeTokenServer(t, nil)
	a := New(WithClientID("cid"), WithClientSecret("sec"), WithEndpoint(ts.URL+"/authorize", ts.URL+"/token"))
	ctx := context.Background()

	r := httptest.NewRequest("GET", "/cb?state=wrong&code=good-code", nil)
	if _, err := a.Callback(ctx, "st", r); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("want state mismatch, got %v", err)
	}
	r = httptest.NewRequest("GET", "/cb?error=access_denied&state=st", nil)
	if _, err := a.Callback(ctx, "st", r); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want access denied, got %v", err)
	}
	r = httptest.NewRequest("GET", "/cb?state=st&code=good-code", nil)
	tok, err := a.Callback(ctx, "st", r)
	if err != nil || tok.AccessToken != "access-authorization_code" {
		t.Fatalf("tok=%v err=%v", tok, err)
	}
	if s := ScopesOf(tok); len(s) != 2 || s[1] != ScopeUserTopRead {
		t.Fatalf("scopes %v", s)
	}
	refreshed, err := a.RefreshToken(ctx, tok)
	if err != nil || refreshed.AccessToken != "access-refresh_token" {
		t.Fatalf("refresh: %v %v", refreshed, err)
	}
}

func TestFileStoreAndPersistentClient(t *testing.T) {
	fs := &FileStore{Path: filepath.Join(t.TempDir(), "sub", "tok.json")}
	if tok, err := fs.Load(); tok != nil || err != nil {
		t.Fatalf("empty load: %v %v", tok, err)
	}
	ts := fakeTokenServer(t, nil)
	a := New(WithClientID("cid"), WithClientSecret("sec"), WithEndpoint(ts.URL+"/a", ts.URL+"/token"))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-refresh_token" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
	}))
	defer api.Close()

	expired := &oauth2.Token{AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour)}
	hc := a.PersistentClient(context.Background(), expired, fs)
	resp, err := hc.Get(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	saved, err := fs.Load()
	if err != nil || saved == nil || saved.AccessToken != "access-refresh_token" {
		t.Fatalf("token not persisted: %v %v", saved, err)
	}
}

func TestLocalLoginEndToEnd(t *testing.T) {
	v := "x"
	ts := fakeTokenServer(t, &v)
	port := freePort(t)
	redirect := "http://127.0.0.1:" + strconv.Itoa(port) + "/callback"
	a := New(WithClientID("cid"), WithRedirectURL(redirect), WithEndpoint(ts.URL+"/authorize", ts.URL+"/token"))
	store := &MemoryStore{}

	// The fake "browser" follows the auth URL by calling the redirect
	// with a code and the state it was given.
	browser := func(authURL string) error {
		u, _ := url.Parse(authURL)
		go func() {
			resp, err := http.Get(redirect + "?code=good-code&state=" + url.QueryEscape(u.Query().Get("state")))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	tok, err := a.LocalLogin(context.Background(), LoginOptions{Store: store, OpenURL: browser, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access-authorization_code" {
		t.Fatalf("tok %v", tok)
	}
	if s, _ := store.Load(); s == nil {
		t.Fatal("token not stored")
	}
	// Second call is served from the store with no browser interaction.
	tok2, err := a.LocalLogin(context.Background(), LoginOptions{Store: store, OpenURL: func(string) error {
		t.Fatal("browser should not open")
		return nil
	}})
	if err != nil || tok2.AccessToken != tok.AccessToken {
		t.Fatalf("cached login: %v %v", tok2, err)
	}
}

func TestLocalLoginRejectsNonLoopback(t *testing.T) {
	a := New(WithRedirectURL("https://example.com/cb"))
	if _, err := a.LocalLogin(context.Background(), LoginOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRedirectPartsLoopback(t *testing.T) {
	for raw, ok := range map[string]bool{
		"http://127.0.0.1:8080/callback": true,
		"http://[::1]:8080/callback":     true,
		"http://localhost:8080":          true,
		"https://127.0.0.1:8080/cb":      false,
		"http://example.com/cb":          false,
	} {
		u, err := redirectParts(raw)
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", raw, err, ok)
		}
		if ok && err == nil && u.Path == "" {
			t.Errorf("%s: empty path not defaulted", raw)
		}
	}
}
