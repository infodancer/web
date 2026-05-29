package main

import (
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml/v2"

	embedding "github.com/matthewjhunter/go-embedding"
)

// fileConfig is the subset of the DESIGN §9 TOML the MVP consumes. Indexing
// keys (hugo_dir, public_dir, [embedding], sources, [reaping]) are accepted but
// unused until the indexer is built; unknown keys are ignored.
type fileConfig struct {
	DB struct {
		IndexerDSN string `toml:"indexer_dsn"` // RW role; used by index/seed/migrate
		QueryDSN   string `toml:"query_dsn"`   // RO role; used by serve
	} `toml:"db"`

	Serve struct {
		Addr     string `toml:"addr"`
		BasePath string `toml:"base_path"`
		SiteName string `toml:"site_name"`
		HomeURL  string `toml:"home_url"`
	} `toml:"serve"`
}

// loadFileConfig reads and parses the TOML config at path. A blank path returns
// a zero config (all-flag/env operation).
func loadFileConfig(path string) (fileConfig, error) {
	var fc fileConfig
	if path == "" {
		return fc, nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		return fc, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, &fc); err != nil {
		return fc, fmt.Errorf("parse config %s: %w", path, err)
	}
	return fc, nil
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildEmbedder constructs a go-embedding Embedder from the EMBEDDING_* env
// (see embedding.ConfigFromEnv). It is opt-in: a nil embedder (and no error) is
// returned unless EMBEDDING_MODEL is set, so a deployment that has not chosen an
// embedding model stays FTS-only rather than dialing a default backend.
func buildEmbedder() (embedding.Embedder, error) {
	if os.Getenv("EMBEDDING_MODEL") == "" {
		return nil, nil
	}
	cfg, err := embedding.ConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("embedding config: %w", err)
	}
	emb, err := embedding.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("embedding backend: %w", err)
	}
	return emb, nil
}

// buildReranker constructs a go-embedding Reranker from the RERANK_* env (see
// embedding.RerankConfigFromEnv). Opt-in: nil (and no error) unless RERANK_MODEL
// is set.
func buildReranker() (embedding.Reranker, error) {
	if os.Getenv("RERANK_MODEL") == "" {
		return nil, nil
	}
	cfg, err := embedding.RerankConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("rerank config: %w", err)
	}
	rr, err := embedding.NewReranker(cfg)
	if err != nil {
		return nil, fmt.Errorf("rerank backend: %w", err)
	}
	return rr, nil
}
