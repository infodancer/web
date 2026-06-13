// Command sitemap-gate is the forced-inclusion gate for the sitemap module: it
// reads a smoke route manifest and fails when a public, indexable route carries
// no sitemap disposition. Add a public page, forget the sitemap, and CI breaks
// until the route is labelled sitemap=<source> (covered) or
// sitemap=exclude:<reason> (consciously out).
//
//	sitemap-gate --manifest routes.json [--mode warn|fail]
//
// It is the sitemap analog of `smolder gate`. It depends on the smoke manifest
// JSON format, not on the smoke package -- the fields it reads are a stable
// subset.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// sitemapLabel is the manifest label key this gate owns.
const sitemapLabel = "sitemap"

// manifest is the subset of a smoke manifest the gate reads.
type manifest struct {
	Routes []route `json:"routes"`
}

type route struct {
	Method       string            `json:"method"`
	Pattern      string            `json:"pattern"`
	Effect       string            `json:"effect"`
	Skip         string            `json:"skip"`
	AuthRequired bool              `json:"auth_required"`
	Labels       map[string]string `json:"labels"`
}

// indexable reports whether a route is a public page the sitemap should account
// for: a GET (or method-less, which smoke probes as GET), read-only, not
// auth-gated, not skipped. Everything else (writes, auth-only reads, skipped
// routes, non-GET methods) is irrelevant to a sitemap and needs no disposition.
func (r route) indexable() bool {
	m := strings.ToUpper(r.Method)
	if m != "" && m != "GET" {
		return false
	}
	if r.Effect == "mutating" {
		return false
	}
	if r.AuthRequired {
		return false
	}
	if r.Skip != "" {
		return false
	}
	return true
}

// disposition returns the trimmed sitemap label value, or "" when absent.
func (r route) disposition() string {
	return strings.TrimSpace(r.Labels[sitemapLabel])
}

// uncovered returns the indexable routes that carry no sitemap disposition.
func uncovered(m manifest) []route {
	var out []route
	for _, r := range m.Routes {
		if r.indexable() && r.disposition() == "" {
			out = append(out, r)
		}
	}
	return out
}

// parseManifest decodes a smoke manifest from JSON.
func parseManifest(data []byte) (manifest, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

// effectiveMethod is the method to display -- GET when none was recorded.
func (r route) effectiveMethod() string {
	if r.Method == "" {
		return "GET"
	}
	return r.Method
}
