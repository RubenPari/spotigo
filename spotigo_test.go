package spotigo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	fast := RetryPolicy{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, MaxWait: time.Second}
	opts = append([]Option{WithBaseURL(srv.URL + "/v1"), WithRetry(fast)}, opts...)
	return New(srv.Client(), opts...), srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestRetryReplaysBody(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "spotify:track:abc") {
			t.Errorf("attempt %d: body not replayed: %q", calls.Load(), b)
		}
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"error": map[string]any{"status": 429, "message": "slow down"}})
			return
		}
		writeJSON(w, 201, map[string]string{"snapshot_id": "snap"})
	})
	snap, err := c.AddItems(context.Background(), "pl", "spotify:track:abc")
	if err != nil {
		t.Fatal(err)
	}
	if snap != "snap" || calls.Load() != 3 {
		t.Fatalf("snap=%q calls=%d", snap, calls.Load())
	}
}

func TestRetryGivesUp(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, map[string]any{"error": map[string]any{"status": 503, "message": "down"}})
	})
	_, err := c.GetTrack(context.Background(), "x")
	if !errors.Is(err, ErrServer) {
		t.Fatalf("want ErrServer, got %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("calls=%d, want 4", calls.Load())
	}
}

func TestRetryAfterTooLong(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, map[string]any{"error": map[string]any{"status": 429, "message": "Too many requests", "reason": "QUOTA_EXCEEDED"}})
	})
	_, err := c.GetTrack(context.Background(), "x")
	e, ok := AsError(err)
	if !ok || !errors.Is(err, ErrRateLimited) || !errors.Is(err, ErrQuotaExceeded) || e.RetryAfter != time.Hour {
		t.Fatalf("unexpected: %#v", err)
	}
}

func TestContextCancelDuringBackoff(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetTrack(ctx, "x")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestErrorDecoding(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tracks/missing":
			writeJSON(w, 404, map[string]any{"error": map[string]any{"status": 404, "message": "Non existing id"}})
		case "/v1/me/player/pause":
			writeJSON(w, 404, map[string]any{"error": map[string]any{"status": 404, "message": "Player command failed", "reason": "NO_ACTIVE_DEVICE"}})
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte("plain text"))
		}
	})
	ctx := context.Background()
	_, err := c.GetTrack(ctx, "missing")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "Non existing id") {
		t.Fatalf("got %v", err)
	}
	if err := c.Pause(ctx, ""); !errors.Is(err, ErrNoActiveDevice) {
		t.Fatalf("got %v", err)
	}
	_, err = c.CurrentUser(ctx)
	if e, _ := AsError(err); !errors.Is(err, ErrUnauthorized) || e.Message != "plain text" {
		t.Fatalf("got %v", err)
	}
}

func TestPaginate(t *testing.T) {
	var base string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		off := r.URL.Query().Get("offset")
		next := ""
		var items []map[string]any
		switch off {
		case "", "0":
			next = base + "/v1/me/tracks?offset=2&limit=2"
			items = []map[string]any{{"track": map[string]any{"name": "a"}}, {"track": map[string]any{"name": "b"}}}
		case "2":
			next = base + "/v1/me/tracks?offset=4&limit=2"
			items = []map[string]any{{"track": map[string]any{"name": "c"}}, {"track": map[string]any{"name": "d"}}}
		case "4":
			items = []map[string]any{{"track": map[string]any{"name": "e"}}}
		}
		writeJSON(w, 200, map[string]any{"items": items, "next": next, "total": 5})
	})
	base = srv.URL
	ctx := context.Background()
	all, err := Collect(c.AllSavedTracks(ctx))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Track.Name)
	}
	if strings.Join(names, "") != "abcde" {
		t.Fatalf("got %v", names)
	}
	// Early break must stop fetching.
	got, _ := Collect(Take(c.AllSavedTracks(ctx), 3))
	if len(got) != 3 {
		t.Fatalf("take: %d", len(got))
	}
}

func TestSearchPagingUnwrapsEnvelope(t *testing.T) {
	var base string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "track" {
			t.Errorf("type=%q", r.URL.Query().Get("type"))
		}
		if r.URL.Query().Get("offset") == "1" {
			writeJSON(w, 200, map[string]any{"tracks": map[string]any{"items": []any{map[string]any{"name": "two"}}}})
			return
		}
		writeJSON(w, 200, map[string]any{"tracks": map[string]any{
			"items": []any{map[string]any{"name": "one"}},
			"next":  base + "/v1/search?q=x&type=track&offset=1",
		}})
	})
	base = srv.URL
	got, err := Collect(c.SearchTracks(context.Background(), Query{Artist: "Daft Punk"}.String()))
	if err != nil || len(got) != 2 || got[1].Name != "two" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestBatchAutoFallback(t *testing.T) {
	var batchCalls, singleCalls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tracks" {
			batchCalls.Add(1)
			writeJSON(w, 403, map[string]any{"error": map[string]any{"status": 403, "message": "Forbidden"}})
			return
		}
		singleCalls.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/v1/tracks/")
		if id == "gone" {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"status": 404}})
			return
		}
		time.Sleep(time.Millisecond)
		writeJSON(w, 200, map[string]any{"id": id, "name": "n-" + id})
	})
	ctx := context.Background()
	ids := []ID{"a", "b", "gone", "d", "e", "f", "g", "h", "i", "j"}
	got, err := c.GetTracks(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	for i, tr := range got {
		if ids[i] == "gone" {
			if tr != nil {
				t.Fatalf("expected nil for unknown id")
			}
			continue
		}
		if tr == nil || tr.ID != ids[i] {
			t.Fatalf("order not preserved at %d: %+v", i, tr)
		}
	}
	if _, err := c.GetTracks(ctx, []ID{"z"}); err != nil {
		t.Fatal(err)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch endpoint should be tried once, got %d", batchCalls.Load())
	}
	if singleCalls.Load() != 11 {
		t.Fatalf("single calls = %d", singleCalls.Load())
	}
}

func TestBatchChunking(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		if len(ids) > 50 {
			t.Errorf("chunk too large: %d", len(ids))
		}
		if r.URL.Query().Get("market") != "IT" {
			t.Errorf("default market not applied")
		}
		var out []map[string]any
		for _, id := range ids {
			out = append(out, map[string]any{"id": id})
		}
		writeJSON(w, 200, map[string]any{"tracks": out})
	}, WithMarket("IT"))
	ids := make([]ID, 120)
	for i := range ids {
		ids[i] = ID(fmt.Sprintf("id%d", i))
	}
	got, err := c.GetTracks(context.Background(), ids)
	if err != nil || len(got) != 120 || got[119].ID != "id119" || calls.Load() != 3 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), calls.Load(), err)
	}
}

func TestLibraryChunking(t *testing.T) {
	var sizes []int
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/library" || r.Method != http.MethodPut {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		uris := strings.Split(r.URL.Query().Get("uris"), ",")
		if !strings.HasPrefix(uris[0], "spotify:track:") {
			t.Errorf("bad uri %q", uris[0])
		}
		sizes = append(sizes, len(uris))
		w.WriteHeader(200)
	})
	ids := make([]ID, 85)
	for i := range ids {
		ids[i] = ID(fmt.Sprintf("t%d", i))
	}
	if err := c.SaveTracks(context.Background(), ids...); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[40 40 5]" {
		t.Fatalf("sizes=%v", sizes)
	}
}

func TestReplaceItemsLarge(t *testing.T) {
	var log []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URIs []string `json:"uris"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		log = append(log, fmt.Sprintf("%s:%d", r.Method, len(body.URIs)))
		writeJSON(w, 200, map[string]string{"snapshot_id": fmt.Sprint(len(log))})
	})
	uris := make([]URI, 250)
	for i := range uris {
		uris[i] = NewURI(TypeTrack, ID(fmt.Sprintf("t%d", i)))
	}
	snap, err := c.ReplaceItems(context.Background(), "pl", uris...)
	if err != nil || snap != "3" || strings.Join(log, " ") != "PUT:100 POST:100 POST:50" {
		t.Fatalf("snap=%q log=%v err=%v", snap, log, err)
	}
}

func TestPlaylistDecodingNewAndLegacy(t *testing.T) {
	newer := `{"id":"p","name":"Mix","items":{"total":2,"items":[
		{"added_at":"2026-01-02T03:04:05Z","item":{"type":"track","name":"Song","uri":"spotify:track:1","duration_ms":1000}},
		{"item":{"type":"episode","name":"Pod","uri":"spotify:episode:2"}}]}}`
	legacy := `{"id":"p","name":"Mix","tracks":{"total":2,"items":[
		{"added_at":"2026-01-02T03:04:05Z","track":{"type":"track","name":"Song","uri":"spotify:track:1","duration_ms":1000}},
		{"track":{"type":"episode","name":"Pod","uri":"spotify:episode:2"}}]}}`
	for name, raw := range map[string]string{"new": newer, "legacy": legacy} {
		var p FullPlaylist
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Name != "Mix" || p.Items.Total != 2 || len(p.Content.Items) != 2 {
			t.Fatalf("%s: %+v", name, p)
		}
		it := p.Content.Items
		if it[0].Item.Track == nil || it[0].Item.Name() != "Song" || it[0].Item.Duration() != time.Second || it[0].AddedAt.Year() != 2026 {
			t.Fatalf("%s: track: %+v", name, it[0])
		}
		if it[1].Item.Episode == nil || it[1].Item.URI() != "spotify:episode:2" {
			t.Fatalf("%s: episode: %+v", name, it[1])
		}
	}
	var sp SimplePlaylist
	_ = json.Unmarshal([]byte(`{"name":"x","tracks":{"href":"h","total":7}}`), &sp)
	if sp.Items.Total != 7 || sp.Name != "x" {
		t.Fatalf("simple legacy: %+v", sp)
	}
}

func TestPlayerNothingPlaying(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	st, err := c.PlaybackState(context.Background())
	if st != nil || err != nil {
		t.Fatalf("st=%v err=%v", st, err)
	}
}

func TestPlayBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("device_id") != "dev" {
			t.Errorf("device id missing")
		}
		b, _ := io.ReadAll(r.Body)
		want := `{"context_uri":"spotify:album:x","offset":{"position":3},"position_ms":1500}`
		if strings.TrimSpace(string(b)) != want {
			t.Errorf("body=%s", b)
		}
		w.WriteHeader(204)
	})
	err := c.Play(context.Background(), &PlayOptions{DeviceID: "dev", ContextURI: "spotify:album:x", Offset: AtPosition(3), Position: 1500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHeaders(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Language") != "it" || !strings.HasPrefix(r.Header.Get("User-Agent"), "spotigo/") {
			t.Errorf("headers: %v", r.Header)
		}
		writeJSON(w, 200, map[string]any{"id": "me", "account_id": "acc"})
	}, WithAcceptLanguage("it"))
	u, err := c.CurrentUser(context.Background())
	if err != nil || u.AccountID != "acc" {
		t.Fatalf("%v %v", u, err)
	}
}

func TestParseURI(t *testing.T) {
	cases := []struct {
		in   string
		typ  ItemType
		id   ID
		fail bool
	}{
		{"spotify:track:6rqhFgbbKwnb9MLmUQDhG6", TypeTrack, "6rqhFgbbKwnb9MLmUQDhG6", false},
		{"https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6?si=abc", TypeTrack, "6rqhFgbbKwnb9MLmUQDhG6", false},
		{"https://open.spotify.com/intl-it/album/1ATL5GLyefJaxhQzSPVrLX", TypeAlbum, "1ATL5GLyefJaxhQzSPVrLX", false},
		{"spotify:user:alice:playlist:37i9dQZF1DXcBWIGoYBM5M", TypePlaylist, "37i9dQZF1DXcBWIGoYBM5M", false},
		{"https://open.spotify.com/user/alice/playlist/37i9dQZF1DXcBWIGoYBM5M", TypePlaylist, "37i9dQZF1DXcBWIGoYBM5M", false},
		{"https://example.com/track/abc", "", "", true},
		{"spotify:track:bad-id!", "", "", true},
		{"spotify:widget:abc", "", "", true},
	}
	for _, c := range cases {
		typ, id, err := ParseURI(c.in)
		if (err != nil) != c.fail {
			t.Errorf("%s: err=%v", c.in, err)
			continue
		}
		if !c.fail && (typ != c.typ || id != c.id) {
			t.Errorf("%s: got %s %s", c.in, typ, id)
		}
	}
	if id, err := ParseID("6rqhFgbbKwnb9MLmUQDhG6"); err != nil || id != "6rqhFgbbKwnb9MLmUQDhG6" {
		t.Error("ParseID bare")
	}
	if URI("spotify:album:abc").ID() != "abc" || URI("spotify:album:abc").Type() != TypeAlbum {
		t.Error("URI helpers")
	}
}

func TestQueryAndDates(t *testing.T) {
	q := Query{Text: "remix", Artist: "Daft Punk", Year: "2001", Tag: "new"}.String()
	if q != `remix artist:"Daft Punk" year:2001 tag:new` {
		t.Fatalf("query %q", q)
	}
	if ReleaseDate("1997").Year() != 1997 || ReleaseDate("1997-05").Year() != 1997 || ReleaseDate("1997-05-21").Year() != 1997 {
		t.Fatal("release date")
	}
	im := Images{{URL: "s", Width: 64}, {URL: "l", Width: 640}, {URL: "m", Width: 300}}
	if im.Largest().URL != "l" || im.Smallest(100).URL != "m" {
		t.Fatal("images")
	}
}

func TestChunk(t *testing.T) {
	got := chunk([]int{1, 2, 3, 4, 5}, 2)
	if fmt.Sprint(got) != "[[1 2] [3 4] [5]]" {
		t.Fatal(got)
	}
	// Appending to a chunk must not clobber the next one.
	got[0] = append(got[0], 99)
	if got[1][0] != 3 {
		t.Fatal("chunk aliasing")
	}
}

func TestPlayResumeSendsNoBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if len(b) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("resume must send no body: ctype=%q body=%q", r.Header.Get("Content-Type"), b)
		}
		w.WriteHeader(204)
	})
	if err := c.Play(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentlyPlayingOptional(t *testing.T) {
	var body string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, body)
	})
	ctx := context.Background()
	cp, err := c.CurrentlyPlaying(ctx)
	if cp != nil || err != nil {
		t.Fatalf("empty body: cp=%v err=%v", cp, err)
	}
	body = `{"is_playing":true,"progress_ms":2000,"item":{"type":"episode","name":"Pod"}}`
	cp, err = c.CurrentlyPlaying(ctx)
	if err != nil || cp == nil || !cp.IsPlaying || cp.Item.Episode == nil || cp.Progress() != 2*time.Second {
		t.Fatalf("cp=%+v err=%v", cp, err)
	}
}

func TestPaginateCursorEnvelope(t *testing.T) {
	var base string
	var limits []string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		limits = append(limits, r.URL.Query().Get("limit"))
		page := map[string]any{"items": []map[string]any{{"name": "a"}}, "next": base + "/v1/me/following?type=artist&after=a"}
		if r.URL.Query().Get("after") == "a" {
			page = map[string]any{"items": []map[string]any{{"name": "b"}}, "next": nil}
		}
		writeJSON(w, 200, map[string]any{"artists": page})
	})
	base = srv.URL
	all, err := Collect(c.AllFollowedArtists(context.Background(), Limit(7)))
	if err != nil || len(all) != 2 || all[0].Name != "a" || all[1].Name != "b" {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	if limits[0] != "7" {
		t.Fatalf("explicit Limit must override the default: %v", limits)
	}
}

func TestPreviousPage(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []string{"x"}, "offset": 0})
	})
	ctx := context.Background()
	if _, err := PreviousPage(ctx, c, &Page[string]{}); !errors.Is(err, ErrNoMorePages) {
		t.Fatalf("want ErrNoMorePages, got %v", err)
	}
	p, err := PreviousPage(ctx, c, &Page[string]{Previous: srv.URL + "/v1/x?offset=0"})
	if err != nil || len(p.Items) != 1 || p.Items[0] != "x" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestBatchEndpointModeDoesNotFallBack(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 403, map[string]any{"error": map[string]any{"status": 403, "message": "nope"}})
	}, WithBatchMode(BatchEndpoint))
	_, err := c.GetTracks(context.Background(), []ID{"a", "b"})
	if e, ok := AsError(err); !ok || e.Status != 403 || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestNonIdempotentNotRetriedAfterGatewayError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 502, map[string]any{"error": map[string]any{"status": 502, "message": "bad gateway"}})
	})
	ctx := context.Background()
	// POST: Spotify may already have skipped the track; do not skip twice.
	if err := c.Next(ctx, ""); !errors.Is(err, ErrServer) || calls.Load() != 1 {
		t.Fatalf("POST: err=%v calls=%d, want 1 call", err, calls.Load())
	}
	// GET is safe to repeat.
	calls.Store(0)
	if _, err := c.GetTrack(ctx, "x"); !errors.Is(err, ErrServer) || calls.Load() != 4 {
		t.Fatalf("GET: err=%v calls=%d, want 4 calls", err, calls.Load())
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, tc := range []struct {
		method string
		code   int
		want   bool
	}{
		{http.MethodPost, 429, true},
		{http.MethodPost, 503, true},
		{http.MethodPost, 502, false},
		{http.MethodPost, 504, false},
		{http.MethodPut, 502, true},
		{http.MethodDelete, 504, true},
		{http.MethodGet, 500, false},
	} {
		if got := retryableStatus(tc.method, tc.code); got != tc.want {
			t.Errorf("%s %d: got %v", tc.method, tc.code, got)
		}
	}
}

func TestRetryableTransport(t *testing.T) {
	wrap := func(op string, err error) error {
		return &url.Error{Op: "Get", URL: "https://x", Err: &net.OpError{Op: op, Net: "tcp", Err: err}}
	}
	reset := &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}
	for _, tc := range []struct {
		name   string
		method string
		err    error
		want   bool
	}{
		{"refused", http.MethodGet, wrap("dial", &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}), false},
		{"dial failure POST", http.MethodPost, wrap("dial", errors.New("network is unreachable")), true},
		{"reset GET", http.MethodGet, wrap("read", reset), true},
		{"reset POST", http.MethodPost, wrap("read", reset), false},
		{"other", http.MethodGet, errors.New("boom"), false},
	} {
		if got := retryableTransport(tc.method, tc.err); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestConnectionRefusedFailsFast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c := New(nil, WithBaseURL("http://"+addr), WithRetry(RetryPolicy{MaxRetries: 3, BaseDelay: time.Second}))
	start := time.Now()
	if _, err := c.CurrentUser(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("connection refused was retried with backoff (%v)", d)
	}
}

func TestInvalidBaseURL(t *testing.T) {
	for _, raw := range []string{"api.example.com/v1", "://bad", "/relative"} {
		c := New(nil, WithBaseURL(raw))
		_, err := c.CurrentUser(context.Background())
		if err == nil || !strings.Contains(err.Error(), "invalid base URL") {
			t.Errorf("%q: err=%v", raw, err)
		}
	}
	// A later valid option clears the error.
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"id": "me"}) })
	WithBaseURL("bad")(c)
	WithBaseURL(c.baseURL.String())(c)
	if _, err := c.CurrentUser(context.Background()); err != nil {
		t.Fatal(err)
	}
}
