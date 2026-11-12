# spotigo

A modern, batteries-included Go client for the [Spotify Web API](https://developer.spotify.com/documentation/web-api),
**up to date with the 2026 API changes**, plus a handy `spotigo` CLI.

```sh
go get github.com/RubenPari/spotigo
```

Requires Go 1.26+. Single dependency: `golang.org/x/oauth2`.

## Why spotigo?

spotigo is inspired by [`zmb3/spotify`](https://github.com/zmb3/spotify), but redesigned from scratch:

| | zmb3/spotify | **spotigo** |
|---|---|---|
| Paging | reflection, `NextPage(&page)` | generic `Page[T]` + Go 1.23 iterators: `for t, err := range c.AllSavedTracks(ctx)` |
| Retries | 429 only, opt-in, no limit, POST bodies not replayed | 429 / 502 / 503 / 504 / network errors, exponential backoff + jitter, `Retry-After`, max retries, replayable bodies, context-aware; POSTs retried only when they surely had no effect (no duplicate adds/skips) |
| Batch lookups | caller must chunk (max 20/50) | any number of IDs, auto-chunked, order preserved |
| **Feb 2026 API changes** (Development Mode) | removed endpoints → 403 | `/me/library`, `/playlists/{id}/items`, `items`/`item` fields, automatic fallback from batch endpoints to concurrent single fetches |
| Errors | `Error{Message, Status}` | `errors.Is(err, spotigo.ErrNotFound / ErrRateLimited / ErrNoActiveDevice / ErrPremiumRequired / ErrQuotaExceeded ...)`, `Reason`, `RetryAfter`, request info |
| Playlist writes | max 100 items per call | unlimited (auto-chunked), `ReplaceItems` with >100 items |
| URIs & links | — | `ParseURI("https://open.spotify.com/intl-it/track/…")` |
| Content types | tracks, albums, artists, shows, episodes | + **audiobooks, chapters**, saved episodes/audiobooks, mixed track/episode `Playable` |
| Auth | Authorization Code, PKCE helpers | + one-call **loopback login for CLIs**, token stores, auto-persisted refresh, client credentials helper, `Callback` with state/error validation |
| Search | manual `NextTrackResults` per type | `Query{Artist:…, Year:…}` builder and per-type iterators (handles the new max-10 limit transparently) |
| Logging | — | `log/slog` debug logging |

## Quick start

### App-only (client credentials)

```go
ctx := context.Background()
c := spotigo.New(auth.AppClient(ctx, "", "")) // SPOTIFY_ID / SPOTIFY_SECRET

_, id, _ := spotigo.ParseURI("https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6")
track, err := c.GetTrack(ctx, id)
```

### User login (PKCE, no secret) for CLIs and desktop apps

```go
a := auth.New(
    auth.WithRedirectURL("http://127.0.0.1:8888/callback"), // register it in the dashboard
    auth.WithScopes(auth.ScopeUserLibraryRead, auth.ScopeUserTopRead),
)
store, _ := auth.DefaultFileStore("myapp")         // ~/.config/spotigo/myapp.json (0600)
tok, err := a.LocalLogin(ctx, auth.LoginOptions{Store: store})
c := spotigo.New(a.PersistentClient(ctx, tok, store)) // refreshed tokens are saved back
```

### Web servers (Authorization Code)

```go
state := auth.RandomState()
http.Redirect(w, r, a.AuthURL(state), http.StatusFound)
// ... in the callback handler:
tok, err := a.Callback(r.Context(), state, r) // validates state & error params
c := spotigo.New(a.Client(r.Context(), tok))
```

## Iterators

```go
for saved, err := range c.AllSavedTracks(ctx) {
    if err != nil { return err }
    fmt.Println(saved.Track.Name)
}

first100, err := spotigo.Collect(spotigo.Take(c.SearchTracks(ctx, "lofi"), 100))

page, err := c.GetArtistAlbums(ctx, id, spotigo.IncludeGroups("album"))
for album, err := range spotigo.Paginate(ctx, c, page, err) { … }
```

## Errors

```go
err := c.Play(ctx, nil)
switch {
case errors.Is(err, spotigo.ErrNoActiveDevice):  // open Spotify somewhere
case errors.Is(err, spotigo.ErrPremiumRequired): // free account
case errors.Is(err, spotigo.ErrQuotaExceeded):   // dev-mode quota
}
if e, ok := spotigo.AsError(err); ok {
    log.Println(e.Status, e.Message, e.Reason, e.RetryAfter)
}
```

## Client options

```go
c := spotigo.New(httpClient,
    spotigo.WithMarket("from_token"),      // default market for every request
    spotigo.WithAcceptLanguage("it"),      // localized names
    spotigo.WithRetry(spotigo.RetryPolicy{MaxRetries: 6, BaseDelay: time.Second}),
    spotigo.WithBatchMode(spotigo.BatchAuto), // BatchEndpoint for Extended Quota apps
    spotigo.WithConcurrency(8),
    spotigo.WithLogger(slog.Default()),
)
```

## 2026 API compatibility notes

In February 2026 Spotify changed the Web API for **Development Mode** apps (Extended Quota apps are not affected):

* Library writes and checks moved to `PUT|DELETE /me/library` and `GET /me/library/contains` → `SaveToLibrary`, `RemoveFromLibrary`, `LibraryContains` and typed helpers such as `SaveTracks` and `FollowPlaylist`.
* Playlist content moved to `/playlists/{id}/items`, and the `tracks`→`items` and `track`→`item` fields were renamed. spotigo uses the new routes and decodes **both** shapes.
* Batch lookups (`GET /tracks?ids=…`) were removed. With `BatchAuto` spotigo detects this and switches to concurrent single-item requests.
* Search returns at most 10 results per page; the iterators page transparently.
* Removed endpoints (new releases, categories, markets, artist top tracks, other users' profiles and playlists) are still available but marked `Deprecated`.

## CLI

```sh
go install github.com/RubenPari/spotigo/cmd/spotigo@latest
export SPOTIFY_ID=<client id>        # redirect: http://127.0.0.1:8888/callback
spotigo login
spotigo now
spotigo search -type album "random access memories"
spotigo info https://open.spotify.com/album/4m2880jivSbbyEGAKfITCa
spotigo top -range short tracks
spotigo export -format csv > liked.csv
spotigo play spotify:album:4m2880jivSbbyEGAKfITCa
```

## Development

```sh
make test   # go test -race ./...
make lint   # go vet + staticcheck
```

## License

MIT
