package spotigo

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// RetryPolicy controls how failed requests are retried. Requests are retried
// on 429 Too Many Requests, 502/503/504 and transient network errors. The
// Retry-After header sent by Spotify always takes precedence over the
// computed backoff (bounded by MaxWait).
//
// Non-idempotent requests (POST: adding playlist items, queueing, skipping
// tracks, creating playlists) are only retried when the request certainly
// had no effect: on 429 and 503, or when the connection could not be
// established. They are not retried after a 502/504 or a network error
// mid-request, where Spotify may already have applied them, so a retry
// could duplicate the action.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retries (0 disables retrying).
	MaxRetries int
	// BaseDelay is the initial backoff delay.
	BaseDelay time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
	// MaxWait caps how long we are willing to honour a Retry-After header.
	// If Spotify asks us to wait longer, the error is returned instead so the
	// caller can decide. Zero means no cap.
	MaxWait time.Duration
}

// DefaultRetryPolicy retries up to 4 times with exponential backoff starting
// at 500ms, capped at 30s, and honours Retry-After values up to 2 minutes.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxRetries: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second, MaxWait: 2 * time.Minute}
}

// NoRetry disables automatic retries.
func NoRetry() RetryPolicy { return RetryPolicy{} }

// next returns how long to wait before attempt+1, and whether to retry.
func (p RetryPolicy) next(attempt int, resp *http.Response) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if resp != nil {
		if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			if p.MaxWait > 0 && ra > p.MaxWait {
				return 0, false
			}
			return ra, true
		}
	}
	base := p.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	d := base << attempt
	if p.MaxDelay > 0 && (d > p.MaxDelay || d <= 0) {
		d = p.MaxDelay
	}
	// Full jitter in [d/2, d).
	half := d / 2
	if half > 0 {
		d = half + time.Duration(rand.Int64N(int64(half)))
	}
	return d, true
}

func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// idempotent reports whether repeating a request with this method has the
// same effect as sending it once.
func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// retryableStatus reports whether a response with this status may be
// retried for method. 429 and 503 mean the request was not processed; after
// 502/504 a gateway may already have forwarded it, so only idempotent
// requests are retried.
func retryableStatus(method string, code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return idempotent(method)
	}
	return false
}

// retryableTransport reports whether a transport error may be retried for
// method.
func retryableTransport(method string, err error) bool {
	// Nothing is listening: a configuration problem, not a hiccup. Fail fast
	// instead of backing off for seconds.
	if errors.Is(err, syscall.ECONNREFUSED) {
		return false
	}
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "dial" {
		return true // the request never reached the server
	}
	if !idempotent(method) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.As(err, &oe)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
