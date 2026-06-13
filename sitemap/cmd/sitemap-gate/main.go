package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sitemap-gate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("sitemap-gate", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "smoke manifest JSON file path (required)")
	mode := fs.String("mode", "fail", "warn (report, exit 0) or fail (exit non-zero)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" {
		return fmt.Errorf("--manifest is required")
	}
	if *mode != "warn" && *mode != "fail" {
		return fmt.Errorf("--mode must be warn or fail, got %q", *mode)
	}

	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	m, err := parseManifest(data)
	if err != nil {
		return err
	}

	miss := uncovered(m)
	if len(miss) == 0 {
		fmt.Printf("sitemap-gate: all indexable routes have a sitemap disposition (%d routes)\n", len(m.Routes))
		return nil
	}
	fmt.Fprintf(os.Stderr, "sitemap-gate: %d indexable route(s) lack a sitemap disposition:\n", len(miss))
	for _, r := range miss {
		fmt.Fprintf(os.Stderr, "  %-6s %s  (add smoke.Label(\"sitemap\", \"<source>\") or smoke.Label(\"sitemap\", \"exclude:<reason>\"))\n",
			r.effectiveMethod(), r.Pattern)
	}
	if *mode == "fail" {
		return fmt.Errorf("%d undisposed route(s)", len(miss))
	}
	fmt.Fprintln(os.Stderr, "(warn mode: not failing)")
	return nil
}
