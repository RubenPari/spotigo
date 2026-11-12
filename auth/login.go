package auth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// LoginOptions configures LocalLogin.
type LoginOptions struct {
	// Store, if set, is consulted first and receives the new token.
	Store TokenStore
	// OpenURL is called with the authorization URL; typically it opens the
	// browser. If nil the URL is printed to stdout.
	OpenURL func(string) error
	// Timeout bounds how long we wait for the user (default 5 minutes).
	Timeout time.Duration
	// ForceDialog shows the consent dialog even if previously approved.
	ForceDialog bool
}

// LocalLogin performs the Authorization Code + PKCE flow for command-line
// apps: it starts a temporary server on the loopback redirect URL, sends the
// user to Spotify and waits for the callback. If opts.Store already holds a
// token it is returned without user interaction.
//
// The redirect URL (e.g. http://127.0.0.1:8080/callback) must be registered
// in the Spotify developer dashboard. No client secret is required.
func (a *Authenticator) LocalLogin(ctx context.Context, opts LoginOptions) (*oauth2.Token, error) {
	if opts.Store != nil {
		if t, err := opts.Store.Load(); err == nil && t != nil && t.RefreshToken != "" {
			return t, nil
		}
	}
	redirect, err := redirectParts(a.config.RedirectURL)
	if err != nil {
		return nil, err
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	ln, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("auth: listening on %s: %w", redirect.Host, err)
	}

	state := RandomState()
	var aopts []AuthOption
	if opts.ForceDialog {
		aopts = append(aopts, ShowDialog)
	}
	authURL, verifier := a.AuthURLWithPKCE(state, aopts...)

	done := make(chan loginResult, 1)
	mux := http.NewServeMux()
	mux.Handle(redirect.Path, a.callbackHandler(state, verifier, done))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = srv.Shutdown(sctx)
	}()

	// Fall back to printing the URL if there is no opener or it fails.
	if opts.OpenURL == nil || opts.OpenURL(authURL) != nil {
		printURL(authURL)
	}

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("auth: timed out waiting for login")
		}
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		if opts.Store != nil {
			if err := opts.Store.Save(res.tok); err != nil {
				return nil, err
			}
		}
		return res.tok, nil
	}
}

type loginResult struct {
	tok *oauth2.Token
	err error
}

// callbackHandler serves the OAuth redirect: it exchanges the code, shows
// the user a result page and delivers the first result on done.
func (a *Authenticator) callbackHandler(state, verifier string, done chan<- loginResult) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") == "" && r.URL.Query().Get("error") == "" {
			http.NotFound(w, r)
			return
		}
		tok, err := a.Callback(r.Context(), state, r, Verifier(verifier))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, page, "Login failed", html.EscapeString(err.Error()))
		} else {
			fmt.Fprintf(w, page, "Logged in", "You can close this tab and return to the terminal.")
		}
		select {
		case done <- loginResult{tok, err}:
		default:
		}
	}
}

func printURL(u string) {
	fmt.Printf("Open this URL to log in to Spotify:\n\n  %s\n\n", u)
}

const page = `<!doctype html><meta charset="utf-8"><title>spotigo</title>
<body style="font-family:system-ui;background:#121212;color:#fff;display:grid;place-items:center;height:100vh;margin:0">
<div style="text-align:center"><h1 style="color:#1ed760">%s</h1><p>%s</p></div></body>`
