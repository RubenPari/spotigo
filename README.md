# spotigo

[![Go Reference](https://pkg.go.dev/badge/github.com/RubenPari/spotigo.svg)](https://pkg.go.dev/github.com/RubenPari/spotigo)
[![CI](https://github.com/RubenPari/spotigo/actions/workflows/ci.yml/badge.svg)](https://github.com/RubenPari/spotigo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A modern, batteries-included Go client for the [Spotify Web API](https://developer.spotify.com/documentation/web-api),
**up to date with the February 2026 API changes**, plus a handy [`spotigo` CLI](#cli).

- **Typed paging with iterators**: `for t, err := range c.AllSavedTracks(ctx)` fetches page after page for you.
- **Robust retries**: exponential backoff with jitter, `Retry-After` support, and no duplicated writes.
- **No manual chunking**: pass 500 IDs or URIs, get 500 results back in the same order.
- **Inspectable errors**: `errors.Is(err, spotigo.ErrNoActiveDevice)`, plus status, reason and `Retry-After`.
- **Every OAuth flow**, including a one-call loopback login for CLIs and token persistence.
- **Full coverage**: albums, artists, tracks, shows, episodes, audiobooks, chapters, playlists,
  library, player, search, browse and users.

Requires **Go 1.26+**. Single dependency: `golang.org/x/oauth2`. spotigo is pre-1.0 (`v0.x`): the
API may still change between minor versions.

## Contents

- [Installation](#installation)
- [Getting started](#getting-started)
- [Authentication](#authentication)
- [Usage](#usage): [IDs & links](#ids-uris-and-links) · [catalog](#catalog) · [search](#search) ·
  [paging](#paging-and-iterators) · [library](#library) · [playlists](#playlists) ·
  [player](#player) · [user data](#user-data) · [other endpoints](#endpoints-not-wrapped-yet)
- [Error handling](#error-handling)
- [Configuration](#configuration)
- [2026 API changes](#2026-api-changes)
- [CLI](#cli)
- [Comparison with zmb3/spotify](#comparison-with-zmb3spotify)
- [Development](#development)

## Installation

```sh
go get github.com/RubenPari/spotigo
```

The library is split into two packages:

| Package | Purpose |
|---|---|
| `github.com/RubenPari/spotigo` | the API client (`spotigo.Client`) and all models |
| `github.com/RubenPari/spotigo/auth` | OAuth 2.0 flows, scopes and token storage |

`spotigo.New` accepts any `*http.Client` that authenticates its requests, so you can use `auth`
or bring your own OAuth setup.

## Getting started

1. **Create an app** in the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard)
   and note its *Client ID* (and *Client Secret* if you need one, see below).
2. **Register a redirect URI** if users will log in. Spotify only accepts HTTPS URLs or loopback
   IP literals, e.g. `http://127.0.0.1:8888/callback`.
3. **Export your credentials**. `auth` reads them from the environment by default:

   ```sh
   export SPOTIFY_ID=<client id>
   export SPOTIFY_SECRET=<client secret>   # not needed for PKCE logins
   ```

4. **Make a request**:

   ```go
   package main

   import (
       "context"
       "fmt"
       "log"

       "github.com/RubenPari/spotigo"
       "github.com/RubenPari/spotigo/auth"
   )

   func main() {
       ctx := context.Background()
       c := spotigo.New(auth.AppClient(ctx, "", "")) // SPOTIFY_ID / SPOTIFY_SECRET

       _, id, err := spotigo.ParseURI("https://open.spotify.com/track/6rqhFgbbKwnb9MLmUQDhG6")
       if err != nil {
           log.Fatal(err)
       }
       track, err := c.GetTrack(ctx, id)
       if err != nil {
           log.Fatal(err)
       }
       fmt.Println(track.Name, "by", spotigo.ArtistNames(track.Artists))
   }
   ```

## Authentication

Pick the flow that matches your app:

| Flow | Use it for | User data | Secret |
|---|---|---|---|
| [Client credentials](#app-only-client-credentials) | servers and scripts reading public catalog data | no | yes |
| [Loopback login (PKCE)](#clis-and-desktop-apps-pkce) | CLIs and desktop apps | yes | no |
| [Authorization code](#web-servers-authorization-code) | web apps with a backend | yes | yes |

### App-only (client credentials)

```go
httpClient := auth.AppClient(ctx, "", "") // empty strings fall back to SPOTIFY_ID / SPOTIFY_SECRET
c := spotigo.New(httpClient)
```

Tokens are fetched and renewed automatically. Endpoints under `/me` are not available with this flow.

### CLIs and desktop apps (PKCE)

`LocalLogin` runs the whole Authorization Code + PKCE flow. It starts a temporary server on the
redirect URL, sends the user to Spotify and waits for the callback. No client secret is needed.

```go
a := auth.New(
    auth.WithRedirectURL("http://127.0.0.1:8888/callback"), // must be registered in the dashboard
    auth.WithScopes(auth.ScopeUserLibraryRead, auth.ScopeUserTopRead),
)
store, err := auth.DefaultFileStore("myapp") // <user config dir>/spotigo/myapp.json, mode 0600
if err != nil {
    log.Fatal(err)
}
tok, err := a.LocalLogin(ctx, auth.LoginOptions{
    Store:   store,       // if it already holds a token, no browser interaction happens
    OpenURL: openBrowser, // optional: func(url string) error; by default the URL is printed
})
if err != nil {
    log.Fatal(err)
}
c := spotigo.New(a.PersistentClient(ctx, tok, store)) // refreshed tokens are saved back to store
```

`LoginOptions` also has `Timeout` (default 5 minutes) and `ForceDialog`, which asks the user to
approve the app again.

### Web servers (Authorization code)

The `state` parameter protects against CSRF. Generate one per login attempt, keep it in the
user's session (a cookie here), and verify it in the callback. `Callback` checks `state`, handles
`error=access_denied` and exchanges the code.

```go
a := auth.New(
    auth.WithRedirectURL("https://example.com/callback"),
    auth.WithScopes(auth.ScopePlaylistModifyPrivate),
)

http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
    state := auth.RandomState()
    http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, HttpOnly: true, Secure: true, MaxAge: 600})
    http.Redirect(w, r, a.AuthURL(state), http.StatusFound)
})

http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
    cookie, err := r.Cookie("oauth_state")
    if err != nil {
        http.Error(w, "missing state", http.StatusBadRequest)
        return
    }
    tok, err := a.Callback(r.Context(), cookie.Value, r) // ErrStateMismatch, ErrAccessDenied, ErrMissingCode
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    c := spotigo.New(a.Client(context.Background(), tok)) // persist tok to reuse it later
    _ = c
})
```

For a PKCE flow in your own handlers, use `a.AuthURLWithPKCE(state)` and pass the returned
verifier to `Callback`/`Exchange` with `auth.Verifier(v)`.

### Tokens and scopes

- **Storage**: `auth.FileStore` saves tokens atomically to disk, and `auth.MemoryStore` is useful in tests.
  Implement `auth.TokenStore` (`Load`/`Save`) to keep tokens in a database or a keychain.
- **Refresh**: tokens returned by `a.Client` and `a.PersistentClient` refresh themselves.
  `a.RefreshToken(ctx, tok)` forces a refresh.
- **Scopes**: constants such as `auth.ScopeUserLibraryRead` cover every
  [Spotify scope](https://developer.spotify.com/documentation/web-api/concepts/scopes).
  `auth.ScopesOf(tok)` lists the scopes granted to a token. `auth.AllScopes()` is handy for
  prototyping, but request only what you need in production.

## Usage

All methods take a `context.Context` and are safe to call concurrently from multiple goroutines.
Many accept per-request options (see [request options](#request-options)).

### IDs, URIs and links

```go
t, id, err := spotigo.ParseURI("https://open.spotify.com/intl-it/album/4m2880jivSbbyEGAKfITCa?si=x")
// t == spotigo.TypeAlbum, id == "4m2880jivSbbyEGAKfITCa"

id, err = spotigo.ParseID("4m2880jivSbbyEGAKfITCa") // also accepts URIs and links
uri := spotigo.NewURI(spotigo.TypeTrack, id)         // "spotify:track:…"
uri.Type(); uri.ID()
```

`ParseURI` understands `spotify:` URIs, legacy `spotify:user:…:playlist:…` URIs and
`open.spotify.com` links, including localized `/intl-xx/` links. Invalid input returns an error
wrapping `spotigo.ErrInvalidURI`.

### Catalog

```go
album, err := c.GetAlbum(ctx, id, spotigo.Market("IT"))
tracks, err := c.GetAlbumTracks(ctx, id, spotigo.Limit(50))
artist, err := c.GetArtist(ctx, id)
albums, err := c.GetArtistAlbums(ctx, id, spotigo.IncludeGroups("album", "single"))
show, err := c.GetShow(ctx, id)
book, err := c.GetAudiobook(ctx, id)
```

**Batch lookups** accept any number of IDs and preserve their order. Unknown IDs yield `nil` entries:

```go
tracks, err := c.GetTracks(ctx, ids)        // []ID; also GetAlbums, GetShows, GetEpisodes, GetAudiobooks, GetChapters
artists, err := c.GetArtists(ctx, ids...)   // variadic
```

Requests are chunked to the endpoint limits (20 albums, 50 for everything else). When the batch
endpoints are unavailable, as for Development Mode apps since 2026, spotigo issues concurrent
single-item requests instead (see [`BatchMode`](#batch-mode)).

Helpers on the models include `spotigo.ArtistNames(t.Artists)`, `t.Duration()`, `t.ISRC()`,
`album.Images.Largest()`, `album.Images.Smallest(300)` and `album.ReleaseDate.Year()`.

### Search

```go
// One page per requested type.
res, err := c.Search(ctx, "daft punk", []spotigo.ItemType{spotigo.TypeArtist, spotigo.TypeAlbum}, spotigo.Limit(10))

// Every result, paging transparently (Development Mode apps get max 10 per page).
q := spotigo.Query{Artist: "Daft Punk", Year: "2001"}.String() // artist:"Daft Punk" year:2001
for t, err := range c.SearchTracks(ctx, q) {
    if err != nil {
        return err
    }
    fmt.Println(t.Name)
}
```

The iterators are `SearchTracks`, `SearchArtists`, `SearchAlbums`, `SearchPlaylists`,
`SearchShows`, `SearchEpisodes` and `SearchAudiobooks`. `null` entries that Spotify sometimes
returns are skipped. `Query` supports `Text`, `Artist`, `Album`, `Track`, `Year` (`"1990-1999"`
ranges too), `Genre`, `ISRC`, `UPC` and `Tag` (`"new"`, `"hipster"`).

### Paging and iterators

Paged endpoints return a `*spotigo.Page[T]` (offset-based) or a `*spotigo.CursorPage[T]`
(cursor-based). The `All*` methods return range-over-func iterators that fetch 50 items per
request:

```go
for saved, err := range c.AllSavedTracks(ctx) {
    if err != nil {
        return err
    }
    fmt.Println(saved.AddedAt.Format(time.DateOnly), saved.Track.Name)
}
```

Stopping the loop early (`break`, `return`) stops fetching. Any paged call can be turned into an
iterator, and there are helpers to limit or collect results:

```go
page, err := c.GetArtistAlbums(ctx, id, spotigo.IncludeGroups("album"))
for album, err := range spotigo.Paginate(ctx, c, page, err) { … } // PaginateCursor for CursorPage

first100, err := spotigo.Collect(spotigo.Take(c.SearchTracks(ctx, "lofi"), 100))

next, err := spotigo.NextPage(ctx, c, page) // manual paging: ErrNoMorePages at the end
```

### Library

Saving, removing and checking all go through the unified `/me/library` endpoint and accept any
number of items:

```go
err := c.SaveToLibrary(ctx, trackURI, albumURI, playlistURI) // mix any types
err = c.RemoveFromLibrary(ctx, trackURI)
saved, err := c.LibraryContains(ctx, trackURI, artistURI)     // []bool, same order

// Typed helpers
c.SaveTracks(ctx, ids...); c.RemoveTracks(ctx, ids...); c.HasTracks(ctx, ids...)
c.SaveAlbums(ctx, ids...)  // also Shows, Episodes, Audiobooks
c.FollowArtists(ctx, ids...); c.FollowPlaylist(ctx, id); c.FollowUsers(ctx, "user-id")
```

Listings: `SavedTracks`, `SavedAlbums`, `SavedShows`, `SavedEpisodes`, `SavedAudiobooks`, and the
`AllSavedTracks`/`AllSavedAlbums` iterators.

### Playlists

```go
pl, err := c.CreatePlaylist(ctx, spotigo.PlaylistDetails{
    Name:        "Road trip",
    Description: spotigo.Ptr("Songs for the highway"),
    Public:      spotigo.Ptr(false),
})

snap, err := c.AddItems(ctx, pl.ID, uris...)        // any number: sent 100 at a time
snap, err = c.AddItemsAt(ctx, pl.ID, 0, uris...)    // insert at a position, order preserved
snap, err = c.RemoveItems(ctx, pl.ID, "", uris...)  // "" = latest snapshot
snap, err = c.ReplaceItems(ctx, pl.ID, uris...)     // no URIs clears the playlist
snap, err = c.ReorderItems(ctx, pl.ID, spotigo.Reorder{RangeStart: 5, InsertBefore: 0})

err = c.UpdatePlaylist(ctx, pl.ID, spotigo.PlaylistDetails{Name: "Road trip 2026"}) // nil fields unchanged
err = c.UploadPlaylistCover(ctx, pl.ID, jpegReader) // JPEG ≤ 256 KB, needs ugc-image-upload

for item, err := range c.AllPlaylistItems(ctx, pl.ID) {
    if err != nil {
        return err
    }
    fmt.Println(item.Item.Name(), item.Item.URI()) // Item is a track or an episode
}
```

Use `spotigo.Fields("items(item(name,uri))")` to request only some fields, and
`spotigo.AdditionalTypes(spotigo.TypeTrack, spotigo.TypeEpisode)` to receive podcast episodes.
Since 2026, Spotify only returns playlist contents for playlists the user owns or collaborates on.

### Player

Playback control requires **Spotify Premium** and an active device.

```go
state, err := c.PlaybackState(ctx) // nil, nil when nothing is playing
now, err := c.CurrentlyPlaying(ctx, spotigo.AdditionalTypes(spotigo.TypeTrack, spotigo.TypeEpisode))

err = c.Play(ctx, &spotigo.PlayOptions{
    ContextURI: "spotify:album:4m2880jivSbbyEGAKfITCa",
    Offset:     spotigo.AtPosition(2), // or spotigo.AtURI(trackURI)
    Position:   30 * time.Second,
})
err = c.Play(ctx, nil) // resume

c.Pause(ctx, ""); c.Next(ctx, ""); c.Previous(ctx, "")   // "" = active device
c.Seek(ctx, time.Minute, ""); c.SetVolume(ctx, 40, "")
c.SetShuffle(ctx, true, ""); c.SetRepeat(ctx, spotigo.RepeatContext, "")
c.AddToQueue(ctx, trackURI, "")

devices, err := c.Devices(ctx)
err = c.TransferPlayback(ctx, devices[0].ID, true)
queue, err := c.Queue(ctx)
recent, err := c.RecentlyPlayed(ctx, spotigo.Limit(50), spotigo.AfterTime(time.Now().Add(-24*time.Hour)))
```

### User data

```go
me, err := c.CurrentUser(ctx)
top, err := c.TopTracks(ctx, spotigo.Range(spotigo.ShortTerm), spotigo.Limit(50)) // also TopArtists
for p, err := range c.AllCurrentUserPlaylists(ctx) { … }
for a, err := range c.AllFollowedArtists(ctx) { … }
```

### Endpoints not wrapped yet

`GetRaw` performs an authenticated GET on any path, or on an absolute `href` returned by Spotify,
and decodes the JSON into your type. `spotigo.Param` sets any query parameter:

```go
var out struct{ Name string `json:"name"` }
err := c.GetRaw(ctx, "albums/4m2880jivSbbyEGAKfITCa", &out, spotigo.Param("market", "IT"))
```

## Error handling

API errors are `*spotigo.Error` values (with `Status`, `Message`, `Reason`, `RetryAfter`,
`Method` and `URL`) and match these sentinels with `errors.Is`:

| Sentinel | Matches |
|---|---|
| `ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound` | HTTP 400, 401, 403, 404 |
| `ErrRateLimited` | HTTP 429 |
| `ErrQuotaExceeded` | 429 with reason `QUOTA_EXCEEDED` (Development Mode quota) |
| `ErrServer` | HTTP 5xx |
| `ErrNoActiveDevice` | player call with no active device |
| `ErrPremiumRequired` | player call from a free account |

```go
err := c.Play(ctx, nil)
switch {
case errors.Is(err, spotigo.ErrNoActiveDevice):  // ask the user to open Spotify somewhere
case errors.Is(err, spotigo.ErrPremiumRequired): // free account
case errors.Is(err, spotigo.ErrQuotaExceeded):   // dev-mode quota
}
if e, ok := spotigo.AsError(err); ok {
    log.Println(e.Status, e.Message, e.Reason, e.RetryAfter)
}
```

Other errors: `spotigo.ErrInvalidURI` (parsing), `spotigo.ErrNoMorePages` (manual paging),
`auth.ErrStateMismatch`, `auth.ErrAccessDenied` and `auth.ErrMissingCode` (OAuth callback).

## Configuration

```go
c := spotigo.New(httpClient,
    spotigo.WithMarket("from_token"),         // default market for every request
    spotigo.WithAcceptLanguage("it"),         // localized names and descriptions
    spotigo.WithRetry(spotigo.RetryPolicy{MaxRetries: 6, BaseDelay: time.Second}),
    spotigo.WithBatchMode(spotigo.BatchAuto), // BatchEndpoint for Extended Quota apps
    spotigo.WithConcurrency(8),               // parallel requests when fanning out
    spotigo.WithLogger(slog.Default()),       // debug logs of requests and retries
)
```

| Option | Default |
|---|---|
| `WithMarket` | none. Applied to endpoints that support a market, unless overridden by `spotigo.Market` |
| `WithAcceptLanguage` | none |
| `WithRetry` | `DefaultRetryPolicy()`. `NoRetry()` disables retries |
| `WithBatchMode` | `BatchAuto` |
| `WithConcurrency` | 8 |
| `WithLogger` | no logging |
| `WithUserAgent` | `spotigo/<version>` |
| `WithBaseURL` | `https://api.spotify.com/v1/` (for tests or proxies; must be an absolute URL) |

### Retries

`DefaultRetryPolicy()` retries up to **4 times**, with exponential backoff and jitter starting at
500 ms and capped at 30 s. Retries happen on 429, 502, 503, 504 and transient network errors.
A `Retry-After` header always wins over the computed delay, up to `MaxWait` (2 minutes). If
Spotify asks for a longer wait, the error is returned with `RetryAfter` set so you can decide.
Request bodies are replayed on every attempt, and waits stop as soon as the context is cancelled.

Non-idempotent requests (POST: adding playlist items, queueing, skipping, creating playlists) are
**only retried when they certainly had no effect**: on 429 and 503, or when the connection could
not be established. This means a retry never adds a track twice or skips two songs.

### Batch mode

| Mode | Behaviour |
|---|---|
| `BatchAuto` | Uses the batch endpoints. The first time Spotify rejects them (403/404/410), the client switches permanently to concurrent single-item requests. |
| `BatchEndpoint` | Always uses the batch endpoints (Extended Quota apps). |
| `BatchFanOut` | Always issues concurrent single-item requests. |

### Request options

| Option | Query parameter | Typical endpoints |
|---|---|---|
| `Limit(n)`, `Offset(n)` | `limit`, `offset` | any paged endpoint |
| `Market(code)` | `market` | catalog, playlists, library, player |
| `Country(code)`, `Locale(l)` | `country`, `locale` | browse |
| `Fields(f)` | `fields` | playlists |
| `AdditionalTypes(types...)` | `additional_types` | playlists, player |
| `IncludeGroups(groups...)` | `include_groups` | artist albums |
| `Range(spotigo.ShortTerm)` | `time_range` | top artists/tracks |
| `After`, `Before`, `AfterTime`, `BeforeTime` | `after`, `before` | cursor pages, recently played |
| `DeviceID(id)` | `device_id` | player |
| `IncludeExternalAudio()` | `include_external` | search |
| `Param(key, value)` | anything | escape hatch |

## 2026 API changes

In February 2026 Spotify changed the Web API for **Development Mode** apps. Extended Quota apps
are not affected. spotigo handles the differences for you:

- **Library**: writes and checks moved to `PUT|DELETE /me/library` and `GET /me/library/contains`.
  These are used by `SaveToLibrary`, `RemoveFromLibrary`, `LibraryContains` and the typed helpers.
  `FollowArtists` falls back to the legacy `/me/following` endpoint if needed.
- **Playlists**: content moved to `/playlists/{id}/items`, and the `tracks`→`items` and
  `track`→`item` fields were renamed. spotigo uses the new routes and decodes **both** shapes.
- **Batch lookups** (`GET /tracks?ids=…`) were removed. With `BatchAuto`, spotigo detects this and
  switches to concurrent single-item requests.
- **Search** returns at most 10 results per page. The iterators page transparently.
- **Removed endpoints** (new releases, categories, markets, artist top tracks, other users'
  profiles and playlists) are still available for Extended Quota apps but marked `Deprecated`.
  `GetRelatedArtists` is deprecated too: it has been unavailable to new apps since November 2024.

## CLI

`spotigo` is a small terminal client built on the library.

```sh
go install github.com/RubenPari/spotigo/cmd/spotigo@latest
export SPOTIFY_ID=<client id>   # register http://127.0.0.1:8888/callback as redirect URI
spotigo login                   # opens the browser; no client secret needed
```

| Command | Description |
|---|---|
| `login` / `logout` | log in (Authorization Code + PKCE) / forget the stored token |
| `me` | show your profile |
| `search [-type t] [-n N] <query>` | search; `t` = `track`, `artist`, `album`, `playlist`, `show`, `episode` or `audiobook` |
| `info <uri-or-link>` | details for any Spotify URI or `open.spotify.com` link |
| `top [-range short\|medium\|long] [-n N] tracks\|artists` | your top tracks or artists |
| `liked [-n N]` | your liked songs |
| `playlists` | your playlists |
| `export [-format json\|csv]` | export all liked songs to stdout |
| `like <uri...>` / `unlike <uri...>` | save or remove items |
| `now`, `play [uri]`, `pause`, `next`, `prev`, `vol <0-100>`, `queue <uri...>`, `devices`, `recent [-n N]` | player (Premium) |

```sh
spotigo search -type album "random access memories"
spotigo info https://open.spotify.com/album/4m2880jivSbbyEGAKfITCa
spotigo top -range short tracks
spotigo export -format csv > liked.csv
spotigo play spotify:album:4m2880jivSbbyEGAKfITCa
```

Environment: `SPOTIFY_ID` (required) and `SPOTIFY_REDIRECT` (default
`http://127.0.0.1:8888/callback`). The token is stored in `<user config dir>/spotigo/cli.json`.
Run `spotigo <command> -h` to list a command's flags.

## Comparison with zmb3/spotify

spotigo is inspired by [`zmb3/spotify`](https://github.com/zmb3/spotify), but redesigned from scratch:

| | zmb3/spotify | **spotigo** |
|---|---|---|
| Paging | reflection, `NextPage(&page)` | generic `Page[T]` + Go 1.23 iterators: `for t, err := range c.AllSavedTracks(ctx)` |
| Retries | 429 only, opt-in, no limit, POST bodies not replayed | 429 / 502 / 503 / 504 / network errors, backoff + jitter, `Retry-After`, max retries, replayable bodies, context-aware, no duplicated POSTs |
| Batch lookups | caller must chunk (max 20/50) | any number of IDs, auto-chunked, order preserved |
| **Feb 2026 API changes** (Development Mode) | removed endpoints → 403 | `/me/library`, `/playlists/{id}/items`, `items`/`item` fields, automatic fallback from batch endpoints to concurrent single fetches |
| Errors | `Error{Message, Status}` | `errors.Is(err, spotigo.ErrNotFound / ErrRateLimited / ErrNoActiveDevice / ErrPremiumRequired / ErrQuotaExceeded ...)`, `Reason`, `RetryAfter`, request info |
| Playlist writes | max 100 items per call | unlimited (auto-chunked), `ReplaceItems` with >100 items |
| URIs & links | — | `ParseURI("https://open.spotify.com/intl-it/track/…")` |
| Content types | tracks, albums, artists, shows, episodes | + **audiobooks, chapters**, saved episodes/audiobooks, mixed track/episode `Playable` |
| Auth | Authorization Code, PKCE helpers | + one-call **loopback login for CLIs**, token stores, auto-persisted refresh, client credentials helper, `Callback` with state/error validation |
| Search | manual `NextTrackResults` per type | `Query{Artist:…, Year:…}` builder and per-type iterators (handles the new max-10 limit transparently) |
| Logging | — | `log/slog` debug logging |

## Development

```sh
make test    # go test -race ./...
make lint    # go vet + staticcheck
make build   # builds the CLI into bin/spotigo
make cover   # test coverage summary
```

Runnable examples live in [`example_test.go`](example_test.go) and are rendered on
[pkg.go.dev](https://pkg.go.dev/github.com/RubenPari/spotigo#pkg-examples). Issues and pull
requests are welcome. Please run `make test lint` before submitting.

## License

[MIT](LICENSE) © 2026 RubenPari
