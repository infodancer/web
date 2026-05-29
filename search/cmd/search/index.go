package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/search"
)

// runIndex ingests a directory of Markdown files into the corpus. It is the
// filesystem driver for the in-process ingestion API (DESIGN §4–§5): each *.md
// file becomes one document, and the run reconciles the whole directory against
// the source so files removed on disk are soft-deleted. Uses the read-write
// (indexer) role.
//
// The primary ingestion path is a consuming application calling search.Module's
// Index/Reconcile/Delete in-process; this command is the standalone equivalent
// for filesystem content and a worked example of that API.
func runIndex(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	var (
		configPath   = fs.String("config", "", "path to TOML config")
		dsn          = fs.String("db", "", "Postgres DSN (overrides config/env)")
		dir          = fs.String("dir", "", "directory of Markdown (*.md) files to index (required)")
		baseURL      = fs.String("base-url", "", "URL prefix prepended to each derived path")
		source       = fs.String("source", "fs", "source label stored on each document")
		sourceName   = fs.String("source-name", "", "corpus name within the source (e.g. a site key)")
		allowBulkRea = fs.Bool("allow-bulk-reap", false, "permit soft-deleting >20% of the source in one run")
		reembed      = fs.Bool("reembed", false, "drop and recompute all embeddings on a model/dimension change")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("no --dir given (directory of Markdown files to index)")
	}

	fc, err := loadFileConfig(*configPath)
	if err != nil {
		return err
	}
	db := firstNonEmpty(*dsn, fc.DB.IndexerDSN, os.Getenv("DATABASE_URL"))
	if db == "" {
		return fmt.Errorf("no database DSN (set --db, db.indexer_dsn, or DATABASE_URL)")
	}

	docs, err := collectMarkdown(*dir, *baseURL)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, db)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Optional embedder: when EMBEDDING_MODEL is set, indexing also embeds each
	// document's sections as chunks; otherwise the corpus is FTS-only.
	embedder, err := buildEmbedder()
	if err != nil {
		return err
	}

	m, err := search.New(search.Config{
		DB:                pool,
		IndexerDB:         pool,
		Logger:            logger,
		Embedder:          embedder,
		EmbedMetric:       os.Getenv("SEARCH_EMBED_METRIC"),
		ReembedOnMismatch: *reembed,
		AutoMigrate:       true,
	})
	if err != nil {
		return fmt.Errorf("search.New: %w", err)
	}

	res, err := m.Reconcile(ctx, *source, *sourceName, docs, search.ReconcileOptions{AllowBulkReap: *allowBulkRea})
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	logger.Info("indexed",
		"dir", *dir, "source", *source, "source_name", *sourceName,
		"files", len(docs), "written", res.Indexed, "unchanged", res.Skipped, "deleted", res.Deleted)
	return nil
}

// collectMarkdown walks dir and builds one IngestDocument per *.md file, deriving
// each document's natural key from its directory-relative path and its URL from
// that path against baseURL.
func collectMarkdown(dir, baseURL string) ([]search.IngestDocument, error) {
	var docs []search.IngestDocument
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		body, err := os.ReadFile(path) //nolint:gosec // operator-supplied content directory
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		docs = append(docs, search.IngestDocument{
			Source:     "", // stamped by Reconcile from the run's --source
			ExternalID: rel,
			URL:        deriveURL(baseURL, rel),
			Body:       string(body),
			BodyKind:   search.BodyMarkdown,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, walkErr)
	}
	return docs, nil
}

// deriveURL maps a directory-relative Markdown path to a canonical URL: the .md
// extension is dropped, and an "index" leaf collapses to its containing path so
// "a/index.md" → "{base}/a/" and "index.md" → "{base}/".
func deriveURL(baseURL, rel string) string {
	p := strings.TrimSuffix(rel, filepath.Ext(rel))
	switch {
	case p == "index":
		p = "/"
	case strings.HasSuffix(p, "/index"):
		p = strings.TrimSuffix(p, "index") // keep the trailing slash
	default:
		p = "/" + p
	}
	return strings.TrimRight(baseURL, "/") + p
}
