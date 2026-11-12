// Package spotigo is a modern, batteries-included Go client for the
// Spotify Web API.
//
// Compared to older clients it offers:
//
//   - generic, type-safe paging ([Page], [CursorPage]) with Go 1.23
//     range-over-func iterators ([Paginate], [PaginateCursor]);
//   - robust retries with exponential backoff, jitter, Retry-After support
//     and replayable request bodies ([RetryPolicy]);
//   - automatic chunking of batch endpoints (pass 500 IDs, get 500 results);
//   - rich, inspectable errors ([Error], [ErrNotFound], [ErrRateLimited], ...);
//   - parsing of URIs and open.spotify.com links ([ParseURI]);
//   - full coverage of albums, artists, tracks, shows, episodes, audiobooks,
//     chapters, playlists, library, player, search, browse, markets and users.
//
// A Client needs an *http.Client that authenticates requests; see the auth
// subpackage for helpers implementing every Spotify OAuth2 flow.
package spotigo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Version is the library version, sent in the User-Agent header.
const Version = "0.1.0"

// DefaultBaseURL is the root of the Spotify Web API.
const DefaultBaseURL = "https://api.spotify.com/v1/"

// Client is a Spotify Web API client. It is safe for concurrent use.
type Client struct {
	http           *http.Client
	baseURL        *url.URL
	userAgent      string
	acceptLanguage string
	market         string
	retry          RetryPolicy
	logger         *slog.Logger
	batchMode      BatchMode
	concurrency    int
	noBatch        atomic.Bool
	// configErr is an invalid option detected by New; every request returns it.
	configErr error
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API root (useful for tests or proxies). It must
// be an absolute URL such as "http://127.0.0.1:8080/v1"; otherwise every
// request made by the client fails with an error describing the problem.
func WithBaseURL(raw string) Option {
	return func(c *Client) {
		if !strings.HasSuffix(raw, "/") {
			raw += "/"
		}
		u, err := url.Parse(raw)
		if err == nil && (u.Scheme == "" || u.Host == "") {
			err = errors.New("not an absolute URL")
		}
		if err != nil {
			c.configErr = fmt.Errorf("spotigo: invalid base URL %q: %w", raw, err)
			return
		}
		c.baseURL, c.configErr = u, nil
	}
}

// WithUserAgent sets a custom User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithAcceptLanguage sets the Accept-Language header on every request, which
// localises names and descriptions returned by Spotify.
func WithAcceptLanguage(lang string) Option {
	return func(c *Client) { c.acceptLanguage = lang }
}

// WithMarket sets a default market (ISO 3166-1 alpha-2 code or "from_token")
// applied to every request that does not set one explicitly via [Market].
func WithMarket(market string) Option { return func(c *Client) { c.market = market } }

// WithRetry sets the retry policy. Use [NoRetry] to disable retries.
func WithRetry(p RetryPolicy) Option { return func(c *Client) { c.retry = p } }

// WithLogger enables debug logging of requests and retries.
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.logger = l } }

// New returns a Client. httpClient must authenticate its requests (for
// example the client returned by auth.Authenticator.Client). If nil,
// http.DefaultClient is used, which only works against unauthenticated
// test servers.
func New(httpClient *http.Client, opts ...Option) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	base, _ := url.Parse(DefaultBaseURL)
	c := &Client{
		http:        httpClient,
		baseURL:     base,
		userAgent:   "spotigo/" + Version,
		retry:       DefaultRetryPolicy(),
		concurrency: defaultConcurrency,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client { return c.http }

// request describes a single API call.
type request struct {
	method string
	path   string // relative to baseURL, or absolute URL (for paging links)
	query  url.Values
	body   any    // JSON-encoded unless raw is set
	raw    []byte // pre-encoded body, sent with ctype
	ctype  string // Content-Type of raw
	// accept lists additional success status codes beyond 2xx.
	accept []int
}

func (c *Client) resolve(r *request) (string, error) {
	var u *url.URL
	if strings.HasPrefix(r.path, "http://") || strings.HasPrefix(r.path, "https://") {
		parsed, err := url.Parse(r.path)
		if err != nil {
			return "", err
		}
		u = parsed
	} else {
		rel, err := url.Parse(strings.TrimPrefix(r.path, "/"))
		if err != nil {
			return "", err
		}
		u = c.baseURL.ResolveReference(rel)
	}
	if len(r.query) > 0 {
		q := u.Query()
		for k, vs := range r.query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// do performs the request, decoding a JSON response into out (if non-nil).
// It transparently retries according to the client's RetryPolicy. Request
// bodies are buffered so they can be safely replayed.
func (c *Client) do(ctx context.Context, r *request, out any) error {
	if c.configErr != nil {
		return c.configErr
	}
	target, err := c.resolve(r)
	if err != nil {
		return fmt.Errorf("spotigo: building url: %w", err)
	}

	payload := r.raw
	ctype := r.ctype
	if payload == nil && r.body != nil {
		payload, err = json.Marshal(r.body)
		if err != nil {
			return fmt.Errorf("spotigo: encoding body: %w", err)
		}
		ctype = "application/json"
	}

	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, r.method, target, payload, ctype)
		if err != nil {
			return err
		}

		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !retryableTransport(r.method, err) {
				return err
			}
			if wait, ok := c.retry.next(attempt, nil); ok {
				c.debug("transport error, retrying", "url", target, "err", err, "wait", wait)
				if err := sleep(ctx, wait); err != nil {
					return err
				}
				continue
			}
			return err
		}
		c.debug("request", "method", r.method, "url", target, "status", resp.StatusCode, "took", time.Since(start))

		if retryableStatus(r.method, resp.StatusCode) && !accepted(resp.StatusCode, r.accept) {
			if wait, ok := c.retry.next(attempt, resp); ok {
				drain(resp)
				c.debug("retrying", "status", resp.StatusCode, "wait", wait, "attempt", attempt+1)
				if err := sleep(ctx, wait); err != nil {
					return err
				}
				continue
			}
		}

		err = c.handle(resp, r.accept, out)
		drain(resp)
		return err
	}
}

// newRequest builds one attempt of an API call. A fresh body reader is
// created for every attempt so retries replay the payload.
func (c *Client) newRequest(ctx context.Context, method, target string, payload []byte, ctype string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.acceptLanguage != "" {
		req.Header.Set("Accept-Language", c.acceptLanguage)
	}
	return req, nil
}

func (c *Client) handle(resp *http.Response, acceptCodes []int, out any) error {
	ok := (resp.StatusCode >= 200 && resp.StatusCode < 300) || accepted(resp.StatusCode, acceptCodes)
	if !ok {
		return decodeError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("spotigo: reading response: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("spotigo: decoding response: %w", err)
	}
	return nil
}

func (c *Client) debug(msg string, args ...any) {
	if c.logger != nil {
		c.logger.Debug("spotigo: "+msg, args...)
	}
}

func accepted(code int, list []int) bool {
	for _, a := range list {
		if a == code {
			return true
		}
	}
	return false
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// Convenience wrappers.

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.do(ctx, &request{method: http.MethodGet, path: path, query: q}, out)
}

func (c *Client) put(ctx context.Context, path string, q url.Values, body, out any) error {
	return c.do(ctx, &request{method: http.MethodPut, path: path, query: q, body: body}, out)
}

func (c *Client) post(ctx context.Context, path string, q url.Values, body, out any) error {
	return c.do(ctx, &request{method: http.MethodPost, path: path, query: q, body: body}, out)
}

func (c *Client) delete(ctx context.Context, path string, q url.Values, body, out any) error {
	return c.do(ctx, &request{method: http.MethodDelete, path: path, query: q, body: body}, out)
}

// GetRaw performs an authenticated GET against an arbitrary API path (or
// absolute href returned by Spotify) and decodes the JSON into out. It is an
// escape hatch for endpoints not yet wrapped by this library.
func (c *Client) GetRaw(ctx context.Context, path string, out any, opts ...RequestOption) error {
	return c.get(ctx, path, c.params(opts...), out)
}
