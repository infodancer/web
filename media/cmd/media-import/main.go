// Command media-import ingests source images into a media store and prints the
// canonical public URL of each, for ad-hoc and scripted backfills.
//
// It is a thin wrapper over [media.Ingest]: idempotent on content address, safe
// to re-run. Sources are files, directories (non-recursive, image extensions
// only), shell globs, and/or remote --url images. Per source it prints
//
//	<hash>  <url>  (new|exists)
//
// or, with --json, one {source, hash, url, width, height, existed} record so a
// consumer's repoint step can read stdout. --dry-run validates and reports
// without writing (and without resolving URLs, which only the write path mints).
//
// This is operator-run ingest, not an HTTP surface: auth, CSRF, and the
// subject->record mapping are the operator's concern. The blob backend is
// Postgres by default, or the filesystem with --blob fs --blob-dir.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/infodancer/web/media"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "media-import:", err)
		os.Exit(1)
	}
}

type options struct {
	databaseURL  string
	category     string
	subject      string
	blob         string
	blobDir      string
	publicBase   string
	dryRun       bool
	jsonOut      bool
	urls         multiFlag
	fetchTimeout time.Duration
}

// multiFlag collects a repeatable string flag (used for --url).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var o options
	fs := flag.NewFlagSet("media-import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.databaseURL, "database-url", "", "PostgreSQL connection URL (required unless --dry-run)")
	fs.StringVar(&o.category, "category", "", "server-side media category (required)")
	fs.StringVar(&o.subject, "subject", "", "owning-record id stored with each object")
	fs.StringVar(&o.blob, "blob", "postgres", "blob backend: postgres|fs")
	fs.StringVar(&o.blobDir, "blob-dir", "", "filesystem blob root (required for --blob fs)")
	fs.StringVar(&o.publicBase, "public-base-url", "", "public URL prefix media builds object URLs from")
	fs.BoolVar(&o.dryRun, "dry-run", false, "validate and report without writing")
	fs.BoolVar(&o.jsonOut, "json", false, "emit one JSON record per source instead of text lines")
	fs.Var(&o.urls, "url", "remote http(s) image URL to ingest (repeatable)")
	fs.DurationVar(&o.fetchTimeout, "fetch-timeout", 30*time.Second, "per-URL fetch timeout")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: media-import [flags] <file|dir|glob>... [--url <u>]...\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if o.category == "" {
		return errors.New("--category is required")
	}
	if o.blob != "postgres" && o.blob != "fs" {
		return fmt.Errorf("--blob must be postgres or fs, got %q", o.blob)
	}
	if o.blob == "fs" && o.blobDir == "" {
		return errors.New("--blob fs requires --blob-dir")
	}

	sources, err := resolveSources(fs.Args(), o.urls)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("no input sources: pass files, directories, globs, or --url")
	}

	// Category and base validity are checked the same way the library does, but
	// fail here with a clear message before opening a database connection.
	cfg := media.Config{Category: o.category, PublicBaseURL: o.publicBase}

	if o.dryRun {
		return dryRun(ctx, cfg, sources, o, stdout, stderr)
	}

	if o.databaseURL == "" {
		return errors.New("--database-url is required (or use --dry-run)")
	}
	pool, err := pgxpool.New(ctx, o.databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	if err := media.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	blobs, err := blobStore(o, pool)
	if err != nil {
		return err
	}
	return ingestAll(ctx, media.NewStore(pool), blobs, cfg, o, sources, stdout, stderr)
}

func blobStore(o options, pool *pgxpool.Pool) (media.BlobStore, error) {
	switch o.blob {
	case "postgres":
		return media.NewPostgresBlobStore(pool), nil
	case "fs":
		return media.NewFilesystemBlobStore(o.blobDir)
	default:
		return nil, fmt.Errorf("unknown blob backend %q", o.blob)
	}
}

// source is one input image: a display name and a lazy opener so a large input
// set is never all held open at once.
type source struct {
	name string
	open func(ctx context.Context, timeout time.Duration) (io.ReadCloser, error)
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

func resolveSources(paths []string, urls []string) ([]source, error) {
	var out []source
	for _, p := range paths {
		files, err := expandPath(p)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			out = append(out, source{name: f, open: func(_ context.Context, _ time.Duration) (io.ReadCloser, error) {
				return os.Open(f) //nolint:gosec // operator-supplied path, this is a CLI
			}})
		}
	}
	for _, u := range urls {
		out = append(out, source{name: u, open: func(ctx context.Context, timeout time.Duration) (io.ReadCloser, error) {
			return fetchURL(ctx, u, timeout)
		}})
	}
	return out, nil
}

// expandPath resolves one positional argument to image files: a directory
// expands to its (non-recursive) image entries, a glob to its file matches, and
// a plain path to itself.
func expandPath(p string) ([]string, error) {
	if info, err := os.Stat(p); err == nil {
		if info.IsDir() {
			return imagesInDir(p)
		}
		return []string{p}, nil
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, fmt.Errorf("bad glob %q: %w", p, err)
	}
	var files []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			files = append(files, m)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no such file, directory, or glob match: %q", p)
	}
	sort.Strings(files)
	return files, nil
}

func imagesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no png/jpg/jpeg/gif/webp images in directory %q", dir)
	}
	sort.Strings(files)
	return files, nil
}

// fetchURL opens an http(s) image source. Process bounds the read, so a hostile
// server cannot stream unbounded bytes; the timeout context is cancelled on Close.
func fetchURL(ctx context.Context, rawURL string, timeout time.Duration) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme %q (want http or https)", u.Scheme)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: URL is an operator-supplied CLI argument (scheme already validated); this is an admin import tool, not a network-facing handler
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("fetch %q: status %s", rawURL, resp.Status)
	}
	return cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	c.cancel()
	return c.ReadCloser.Close()
}

func ingestAll(ctx context.Context, store *media.Store, blobs media.BlobStore, cfg media.Config, o options, sources []source, stdout, stderr io.Writer) error {
	enc := json.NewEncoder(stdout)
	var failed int
	for _, s := range sources {
		rc, err := s.open(ctx, o.fetchTimeout)
		if err != nil {
			failed++
			reportErr(stderr, o.jsonOut, enc, s.name, err)
			continue
		}
		res, err := media.Ingest(ctx, store, blobs, cfg, o.subject, rc)
		_ = rc.Close()
		if err != nil {
			failed++
			reportErr(stderr, o.jsonOut, enc, s.name, err)
			continue
		}
		if o.jsonOut {
			_ = enc.Encode(jsonRecord{
				Source:  s.name,
				Hash:    res.Meta.Hash,
				URL:     res.URL,
				Width:   res.Meta.Width,
				Height:  res.Meta.Height,
				Existed: res.Existed,
			})
		} else {
			_, _ = fmt.Fprintf(stdout, "%s  %s  %s\n", res.Meta.Hash, res.URL, state(res.Existed))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d source(s) failed", failed, len(sources))
	}
	return nil
}

func dryRun(ctx context.Context, cfg media.Config, sources []source, o options, stdout, stderr io.Writer) error {
	enc := json.NewEncoder(stdout)
	var failed int
	for _, s := range sources {
		rc, err := s.open(ctx, o.fetchTimeout)
		if err != nil {
			failed++
			reportErr(stderr, o.jsonOut, enc, s.name, err)
			continue
		}
		p, err := media.Process(rc, cfg)
		_ = rc.Close()
		if err != nil {
			failed++
			reportErr(stderr, o.jsonOut, enc, s.name, err)
			continue
		}
		// No URL: media mints the canonical URL only when it stores the object,
		// and the builder is deliberately internal. Dry-run reports the content
		// address and dimensions a real run would store.
		if o.jsonOut {
			_ = enc.Encode(jsonRecord{Source: s.name, Hash: p.SHA256, Width: p.Width, Height: p.Height})
		} else {
			_, _ = fmt.Fprintf(stdout, "%s  %s  (dry-run)\n", p.SHA256, s.name)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d source(s) failed validation", failed, len(sources))
	}
	return nil
}

// jsonRecord is the --json line shape; a consumer's repoint step keys off
// Source and reads URL.
type jsonRecord struct {
	Source  string `json:"source"`
	Hash    string `json:"hash,omitempty"`
	URL     string `json:"url,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Existed bool   `json:"existed"`
	Error   string `json:"error,omitempty"`
}

func state(existed bool) string {
	if existed {
		return "(exists)"
	}
	return "(new)"
}

// reportErr records a per-source failure: a JSON record on stdout in --json
// mode (so the consumer sees every outcome, via enc), a line on stderr
// otherwise (so a text run's stdout stays clean for parsing).
func reportErr(stderr io.Writer, jsonOut bool, enc *json.Encoder, name string, err error) {
	if jsonOut {
		_ = enc.Encode(jsonRecord{Source: name, Error: err.Error()})
		return
	}
	_, _ = fmt.Fprintf(stderr, "ERROR  %s  %s\n", name, err)
}
