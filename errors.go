package spotigo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Sentinel errors usable with errors.Is against an *Error.
var (
	ErrBadRequest   = errors.New("spotigo: bad request")
	ErrUnauthorized = errors.New("spotigo: unauthorized")
	ErrForbidden    = errors.New("spotigo: forbidden")
	ErrNotFound     = errors.New("spotigo: not found")
	ErrRateLimited  = errors.New("spotigo: rate limited")
	ErrServer       = errors.New("spotigo: server error")
	// ErrNoActiveDevice is returned by player endpoints when no device is active.
	ErrNoActiveDevice = errors.New("spotigo: no active device")
	// ErrPremiumRequired is returned by player endpoints for free accounts.
	ErrPremiumRequired = errors.New("spotigo: premium required")
	// ErrQuotaExceeded is returned when the app exceeded its (Development
	// Mode) quota: a 429 with reason QUOTA_EXCEEDED.
	ErrQuotaExceeded = errors.New("spotigo: quota exceeded")
)

// Error is an error returned by the Spotify Web API.
type Error struct {
	// Status is the HTTP status code.
	Status int `json:"status"`
	// Message is the human readable message from Spotify.
	Message string `json:"message"`
	// Reason is set by player endpoints (e.g. "NO_ACTIVE_DEVICE",
	// "PREMIUM_REQUIRED").
	Reason string `json:"reason,omitempty"`
	// RetryAfter is the duration Spotify asked us to wait (429 responses).
	RetryAfter time.Duration `json:"-"`
	// Method and URL of the failed request.
	Method, URL string `json:"-"`
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "spotigo: %d %s", e.Status, http.StatusText(e.Status))
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Reason != "" {
		b.WriteString(" (" + e.Reason + ")")
	}
	if e.Method != "" {
		fmt.Fprintf(&b, " [%s %s]", e.Method, e.URL)
	}
	return b.String()
}

// Is enables errors.Is(err, spotigo.ErrNotFound) and friends.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrBadRequest:
		return e.Status == http.StatusBadRequest
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	case ErrServer:
		return e.Status >= 500
	case ErrNoActiveDevice:
		return e.Reason == "NO_ACTIVE_DEVICE"
	case ErrPremiumRequired:
		return e.Reason == "PREMIUM_REQUIRED"
	case ErrQuotaExceeded:
		return e.Reason == "QUOTA_EXCEEDED"
	}
	return false
}

func decodeError(resp *http.Response) error {
	e := &Error{Status: resp.StatusCode}
	if resp.Request != nil {
		e.Method = resp.Request.Method
		u := *resp.Request.URL
		u.RawQuery = ""
		e.URL = u.String()
	}
	if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
		e.RetryAfter = ra
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var wrapped struct {
		Error json.RawMessage `json:"error"`
		// OAuth-style errors.
		Description string `json:"error_description"`
	}
	if json.Unmarshal(data, &wrapped) == nil && len(wrapped.Error) > 0 {
		var inner struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		if json.Unmarshal(wrapped.Error, &inner) == nil {
			e.Message, e.Reason = inner.Message, inner.Reason
		} else {
			var s string
			_ = json.Unmarshal(wrapped.Error, &s)
			e.Message = strings.TrimSpace(s + " " + wrapped.Description)
		}
	} else if msg := strings.TrimSpace(string(data)); msg != "" && len(msg) < 512 {
		e.Message = msg
	}
	return e
}

// AsError extracts an *Error from err, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
