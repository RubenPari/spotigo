// Command spotigo is a small Spotify CLI built on the spotigo library.
//
//	export SPOTIFY_ID=<your client id>
//	spotigo login
//	spotigo now
//	spotigo search -type artist "daft punk"
//	spotigo top -range short tracks
//	spotigo export -format csv > liked.csv
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RubenPari/spotigo"
	"github.com/RubenPari/spotigo/auth"
)

const usage = `spotigo - Spotify from your terminal

Usage: spotigo <command> [flags] [args]

Account:
  login                  log in (Authorization Code + PKCE, no secret needed)
  logout                 forget the stored token
  me                     show your profile

Catalog:
  search [-type t] [-n N] <query>   t = track|artist|album|playlist|show|episode|audiobook
  info <uri-or-link>                show details for any Spotify URI or open.spotify.com link

Library:
  top [-range short|medium|long] [-n N] tracks|artists
  liked [-n N]                      list liked songs
  playlists                         list your playlists
  export [-format json|csv]         export all liked songs to stdout
  like <uri...> / unlike <uri...>   save or remove items

Player (Premium):
  now | play [uri] | pause | next | prev | vol <0-100> | queue <uri> | devices | recent

Environment:
  SPOTIFY_ID        client id of your app (required)
  SPOTIFY_REDIRECT  redirect URL (default http://127.0.0.1:8888/callback)
`

var scopes = []string{
	auth.ScopeUserReadPrivate, auth.ScopeUserReadEmail, auth.ScopeUserTopRead,
	auth.ScopeUserLibraryRead, auth.ScopeUserLibraryModify, auth.ScopeUserFollowRead,
	auth.ScopeUserFollowModify,
	auth.ScopePlaylistReadPrivate, auth.ScopePlaylistReadCollaborative,
	auth.ScopeUserReadPlaybackState, auth.ScopeUserModifyPlaybackState,
	auth.ScopeUserReadCurrentlyPlaying, auth.ScopeUserReadRecentlyPlayed,
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "help" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", explain(err))
		os.Exit(1)
	}
}

// runFunc is the body of a command. c and w are nil for commands that do
// not need an API client (see command.noClient).
type runFunc func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, args []string) error

type command struct {
	// setup registers the command's own flags on fs and returns its body,
	// which reads the flag values after parsing.
	setup func(fs *flag.FlagSet) runFunc
	// noClient marks commands that run without being logged in.
	noClient bool
}

// plain wraps a command that takes no flags.
func plain(fn runFunc) func(*flag.FlagSet) runFunc {
	return func(*flag.FlagSet) runFunc { return fn }
}

func nFlag(fs *flag.FlagSet) *int { return fs.Int("n", 20, "number of results") }

var commands = map[string]command{
	"login":  {setup: plain(cmdLogin), noClient: true},
	"logout": {setup: plain(cmdLogout), noClient: true},
	"me":     {setup: plain(cmdMe)},
	"search": {setup: func(fs *flag.FlagSet) runFunc {
		typ := fs.String("type", "track", "search type")
		n := nFlag(fs)
		return func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, args []string) error {
			if len(args) == 0 {
				return errors.New("usage: spotigo search [-type t] <query>")
			}
			return search(ctx, c, w, spotigo.ItemType(*typ), strings.Join(args, " "), *n)
		}
	}},
	"info": {setup: plain(func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, args []string) error {
		if len(args) != 1 {
			return errors.New("usage: spotigo info <uri-or-link>")
		}
		return info(ctx, c, w, args[0])
	})},
	"top": {setup: func(fs *flag.FlagSet) runFunc {
		rng := fs.String("range", "medium", "time range: short, medium, long")
		n := nFlag(fs)
		return func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, args []string) error {
			return top(ctx, c, w, args, spotigo.TimeRange(*rng+"_term"), *n)
		}
	}},
	"liked": {setup: func(fs *flag.FlagSet) runFunc {
		n := nFlag(fs)
		return func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, _ []string) error {
			return printSeq(w, c.AllSavedTracks(ctx), *n, func(s spotigo.SavedTrack) []any {
				return []any{s.AddedAt.Format(time.DateOnly), s.Track.Name, spotigo.ArtistNames(s.Track.Artists), s.Track.URI}
			})
		}
	}},
	"playlists": {setup: plain(func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, _ []string) error {
		return printSeq(w, c.AllCurrentUserPlaylists(ctx), -1, func(p spotigo.SimplePlaylist) []any {
			return []any{p.Name, fmt.Sprintf("%d items", p.Items.Total), p.Owner.DisplayName, p.URI}
		})
	})},
	"export": {setup: func(fs *flag.FlagSet) runFunc {
		format := fs.String("format", "json", "export format: json or csv")
		return func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
			return export(ctx, c, *format)
		}
	}},
	"like":   {setup: plain(cmdLike(true))},
	"unlike": {setup: plain(cmdLike(false))},
	"now":    {setup: plain(cmdNow)},
	"play":   {setup: plain(cmdPlay)},
	"pause": {setup: plain(func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
		return c.Pause(ctx, "")
	})},
	"next": {setup: plain(func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
		return c.Next(ctx, "")
	})},
	"prev": {setup: plain(func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
		return c.Previous(ctx, "")
	})},
	"vol": {setup: plain(func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, args []string) error {
		if len(args) != 1 {
			return errors.New("usage: spotigo vol <0-100>")
		}
		v, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		return c.SetVolume(ctx, v, "")
	})},
	"queue": {setup: plain(func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, args []string) error {
		uris, err := parseURIs(args)
		if err != nil {
			return err
		}
		for _, u := range uris {
			if err := c.AddToQueue(ctx, u, ""); err != nil {
				return err
			}
		}
		return nil
	})},
	"devices": {setup: plain(cmdDevices)},
	"recent": {setup: func(fs *flag.FlagSet) runFunc {
		n := nFlag(fs)
		return func(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, _ []string) error {
			p, err := c.RecentlyPlayed(ctx, spotigo.Limit(min(*n, 50)))
			if err != nil {
				return err
			}
			for _, h := range p.Items {
				fmt.Fprintf(w, "%s\t%s\t%s\n", h.PlayedAt.Local().Format("Jan 02 15:04"), h.Track.Name, spotigo.ArtistNames(h.Track.Artists))
			}
			return nil
		}
	}},
}

func run(ctx context.Context, name string, args []string) error {
	cmd, ok := commands[name]
	if !ok {
		return fmt.Errorf("unknown command %q (see `spotigo help`)", name)
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // parse errors are reported once, by main
	body := cmd.setup(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stderr)
			fmt.Fprintf(os.Stderr, "Usage of %s:\n", name)
			fs.PrintDefaults()
			return nil
		}
		return err
	}
	if cmd.noClient {
		return body(ctx, nil, nil, fs.Args())
	}
	c, err := client(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer w.Flush()
	return body(ctx, c, w, fs.Args())
}

func explain(err error) string {
	switch {
	case errors.Is(err, spotigo.ErrNoActiveDevice):
		return "no active device: open Spotify on a device first (see `spotigo devices`)"
	case errors.Is(err, spotigo.ErrPremiumRequired):
		return "this command requires Spotify Premium"
	case errors.Is(err, spotigo.ErrUnauthorized):
		return "not authorized: run `spotigo login` again"
	}
	return err.Error()
}

func requireClientID() error {
	if os.Getenv("SPOTIFY_ID") == "" {
		return errors.New("SPOTIFY_ID is not set")
	}
	return nil
}

func authenticator() *auth.Authenticator {
	redirect := os.Getenv("SPOTIFY_REDIRECT")
	if redirect == "" {
		redirect = "http://127.0.0.1:8888/callback"
	}
	return auth.New(auth.WithRedirectURL(redirect), auth.WithScopes(scopes...), auth.WithClientSecret(""))
}

func store() (*auth.FileStore, error) { return auth.DefaultFileStore("cli") }

func client(ctx context.Context) (*spotigo.Client, error) {
	if err := requireClientID(); err != nil {
		return nil, err
	}
	st, err := store()
	if err != nil {
		return nil, err
	}
	tok, err := st.Load()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, errors.New("not logged in: run `spotigo login`")
	}
	return spotigo.New(authenticator().PersistentClient(ctx, tok, st), spotigo.WithMarket("from_token")), nil
}

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		if _, err := exec.LookPath("wslview"); err == nil {
			cmd = exec.Command("wslview", u)
		} else {
			cmd = exec.Command("xdg-open", u)
		}
	}
	fmt.Printf("Opening your browser. If it does not open, visit:\n\n  %s\n\n", u)
	return cmd.Start()
}

// ---- Commands ----

func cmdLogin(ctx context.Context, _ *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
	if err := requireClientID(); err != nil {
		return err
	}
	st, err := store()
	if err != nil {
		return err
	}
	_ = os.Remove(st.Path)
	if _, err := authenticator().LocalLogin(ctx, auth.LoginOptions{Store: st, OpenURL: openBrowser}); err != nil {
		return err
	}
	fmt.Println("Logged in. Token saved to", st.Path)
	return nil
}

func cmdLogout(context.Context, *spotigo.Client, *tabwriter.Writer, []string) error {
	st, err := store()
	if err != nil {
		return err
	}
	if err := os.Remove(st.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cmdMe(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, _ []string) error {
	u, err := c.CurrentUser(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Name:\t%s\nID:\t%s\n", u.DisplayName, u.ID)
	if u.Email != "" {
		fmt.Fprintf(w, "Email:\t%s\n", u.Email)
	}
	if u.Product != "" {
		fmt.Fprintf(w, "Plan:\t%s\n", u.Product)
	}
	fmt.Fprintf(w, "URL:\t%s\n", u.ExternalURLs.Spotify())
	return nil
}

func top(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, args []string, r spotigo.TimeRange, n int) error {
	limit := spotigo.Limit(min(n, 50))
	if len(args) > 0 && args[0] == "artists" {
		p, err := c.TopArtists(ctx, spotigo.Range(r), limit)
		if err != nil {
			return err
		}
		for i, a := range p.Items {
			fmt.Fprintf(w, "%d.\t%s\t%s\n", i+1, a.Name, strings.Join(a.Genres, ", "))
		}
		return nil
	}
	p, err := c.TopTracks(ctx, spotigo.Range(r), limit)
	if err != nil {
		return err
	}
	for i, t := range p.Items {
		fmt.Fprintf(w, "%d.\t%s\t%s\n", i+1, t.Name, spotigo.ArtistNames(t.Artists))
	}
	return nil
}

func cmdLike(save bool) runFunc {
	return func(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, args []string) error {
		uris, err := parseURIs(args)
		if err != nil {
			return err
		}
		if save {
			return c.SaveToLibrary(ctx, uris...)
		}
		return c.RemoveFromLibrary(ctx, uris...)
	}
}

func cmdNow(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, _ []string) error {
	cp, err := c.CurrentlyPlaying(ctx, spotigo.AdditionalTypes(spotigo.TypeTrack, spotigo.TypeEpisode))
	if err != nil {
		return err
	}
	if cp == nil || cp.Item.IsZero() {
		fmt.Println("Nothing is playing.")
		return nil
	}
	state := "▶"
	if !cp.IsPlaying {
		state = "⏸"
	}
	by := ""
	if t := cp.Item.Track; t != nil {
		by = " — " + spotigo.ArtistNames(t.Artists)
	}
	fmt.Printf("%s %s%s  [%s / %s]\n", state, cp.Item.Name(), by, fmtDur(cp.Progress()), fmtDur(cp.Item.Duration()))
	return nil
}

func cmdPlay(ctx context.Context, c *spotigo.Client, _ *tabwriter.Writer, args []string) error {
	o := &spotigo.PlayOptions{}
	if len(args) > 0 {
		uris, err := parseURIs(args)
		if err != nil {
			return err
		}
		switch uris[0].Type() {
		case spotigo.TypeTrack, spotigo.TypeEpisode:
			o.URIs = uris
		default:
			o.ContextURI = uris[0]
		}
	}
	return c.Play(ctx, o)
}

func cmdDevices(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, _ []string) error {
	ds, err := c.Devices(ctx)
	if err != nil {
		return err
	}
	for _, d := range ds {
		active := ""
		if d.IsActive {
			active = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", active, d.Name, d.Type, d.ID)
	}
	return nil
}

// printSeq writes one tab-separated row per item of seq, stopping after n
// items (n < 0 means no limit) or at the first error.
func printSeq[T any](w io.Writer, seq iter.Seq2[T, error], n int, row func(T) []any) error {
	if n >= 0 {
		seq = spotigo.Take(seq, n)
	}
	for it, err := range seq {
		if err != nil {
			return err
		}
		cols := row(it)
		for i, v := range cols {
			if i > 0 {
				io.WriteString(w, "\t")
			}
			fmt.Fprint(w, v)
		}
		io.WriteString(w, "\n")
	}
	return nil
}

func parseURIs(args []string) ([]spotigo.URI, error) {
	if len(args) == 0 {
		return nil, errors.New("expected at least one Spotify URI or link")
	}
	out := make([]spotigo.URI, len(args))
	for i, a := range args {
		t, id, err := spotigo.ParseURI(a)
		if err != nil {
			return nil, err
		}
		out[i] = spotigo.NewURI(t, id)
	}
	return out, nil
}

func search(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, t spotigo.ItemType, q string, n int) error {
	switch t {
	case spotigo.TypeTrack:
		return printSeq(w, c.SearchTracks(ctx, q), n, func(it spotigo.FullTrack) []any {
			return []any{it.Name, spotigo.ArtistNames(it.Artists), it.Album.Name, it.URI}
		})
	case spotigo.TypeArtist:
		return printSeq(w, c.SearchArtists(ctx, q), n, func(it spotigo.FullArtist) []any {
			return []any{it.Name, strings.Join(it.Genres, ", "), it.URI}
		})
	case spotigo.TypeAlbum:
		return printSeq(w, c.SearchAlbums(ctx, q), n, func(it spotigo.SimpleAlbum) []any {
			return []any{it.Name, spotigo.ArtistNames(it.Artists), it.ReleaseDate.Year(), it.URI}
		})
	case spotigo.TypePlaylist:
		return printSeq(w, c.SearchPlaylists(ctx, q), n, func(it spotigo.SimplePlaylist) []any {
			return []any{it.Name, it.Owner.DisplayName, it.URI}
		})
	case spotigo.TypeShow:
		return printSeq(w, c.SearchShows(ctx, q), n, func(it spotigo.SimpleShow) []any {
			return []any{it.Name, it.URI}
		})
	case spotigo.TypeEpisode:
		return printSeq(w, c.SearchEpisodes(ctx, q), n, func(it spotigo.SimpleEpisode) []any {
			return []any{it.ReleaseDate, it.Name, it.URI}
		})
	case spotigo.TypeAudiobook:
		return printSeq(w, c.SearchAudiobooks(ctx, q), n, func(it spotigo.SimpleAudiobook) []any {
			return []any{it.Name, it.URI}
		})
	}
	return fmt.Errorf("unsupported type %q", t)
}

func info(ctx context.Context, c *spotigo.Client, w *tabwriter.Writer, s string) error {
	t, id, err := spotigo.ParseURI(s)
	if err != nil {
		return err
	}
	switch t {
	case spotigo.TypeTrack:
		tr, err := c.GetTrack(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Track:\t%s\nArtists:\t%s\nAlbum:\t%s (%d)\nDuration:\t%s\nExplicit:\t%v\nURL:\t%s\n",
			tr.Name, spotigo.ArtistNames(tr.Artists), tr.Album.Name, tr.Album.ReleaseDate.Year(), fmtDur(tr.Duration()), tr.Explicit, tr.ExternalURLs.Spotify())
	case spotigo.TypeAlbum:
		al, err := c.GetAlbum(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Album:\t%s\nArtists:\t%s\nReleased:\t%s\nTracks:\t%d\n", al.Name, spotigo.ArtistNames(al.Artists), al.ReleaseDate, al.TotalTracks)
		for _, tr := range al.Tracks.Items {
			fmt.Fprintf(w, "  %d.\t%s\t%s\n", tr.TrackNumber, tr.Name, fmtDur(tr.Duration()))
		}
	case spotigo.TypeArtist:
		ar, err := c.GetArtist(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Artist:\t%s\nGenres:\t%s\nURL:\t%s\n", ar.Name, strings.Join(ar.Genres, ", "), ar.ExternalURLs.Spotify())
	case spotigo.TypePlaylist:
		pl, err := c.GetPlaylist(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Playlist:\t%s\nOwner:\t%s\nItems:\t%d\nDescription:\t%s\n", pl.Name, pl.Owner.DisplayName, pl.Items.Total, pl.Description)
	case spotigo.TypeShow:
		sh, err := c.GetShow(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Show:\t%s\nEpisodes:\t%d\nDescription:\t%s\n", sh.Name, sh.TotalEpisodes, sh.Description)
	case spotigo.TypeEpisode:
		ep, err := c.GetEpisode(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Episode:\t%s\nShow:\t%s\nReleased:\t%s\nDuration:\t%s\n", ep.Name, ep.Show.Name, ep.ReleaseDate, fmtDur(ep.Duration()))
	case spotigo.TypeAudiobook:
		ab, err := c.GetAudiobook(ctx, id)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Audiobook:\t%s\nChapters:\t%d\n", ab.Name, ab.TotalChapters)
	default:
		return fmt.Errorf("info not supported for %s", t)
	}
	return nil
}

func export(ctx context.Context, c *spotigo.Client, format string) error {
	type row struct {
		AddedAt time.Time `json:"added_at"`
		Name    string    `json:"name"`
		Artists string    `json:"artists"`
		Album   string    `json:"album"`
		ISRC    string    `json:"isrc,omitempty"`
		URI     string    `json:"uri"`
	}
	var rows []row
	for s, err := range c.AllSavedTracks(ctx) {
		if err != nil {
			return err
		}
		rows = append(rows, row{s.AddedAt, s.Track.Name, spotigo.ArtistNames(s.Track.Artists), s.Track.Album.Name, s.Track.ISRC(), string(s.Track.URI)})
		if len(rows)%500 == 0 {
			fmt.Fprintf(os.Stderr, "\rexported %d tracks...", len(rows))
		}
	}
	fmt.Fprintf(os.Stderr, "\rexported %d tracks.   \n", len(rows))
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "csv":
		cw := csv.NewWriter(os.Stdout)
		_ = cw.Write([]string{"added_at", "name", "artists", "album", "isrc", "uri"})
		for _, r := range rows {
			_ = cw.Write([]string{r.AddedAt.Format(time.RFC3339), r.Name, r.Artists, r.Album, r.ISRC, r.URI})
		}
		cw.Flush()
		return cw.Error()
	}
	return fmt.Errorf("unknown format %q", format)
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
