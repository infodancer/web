package main

import (
	"os"
	"testing"
)

const sampleManifest = `{"routes":[
  {"method":"GET","pattern":"/book/{slug}","effect":"read_only","labels":{"sitemap":"catalog"}},
  {"method":"GET","pattern":"/login","effect":"read_only","labels":{"sitemap":"exclude:auth page"}},
  {"method":"GET","pattern":"/browse","effect":"read_only"},
  {"method":"POST","pattern":"/subscribe","effect":"mutating"},
  {"method":"GET","pattern":"/account","effect":"read_only","auth_required":true},
  {"method":"GET","pattern":"/admin","effect":"read_only","skip":"admin only"},
  {"pattern":"/health","effect":"read_only","labels":{"sitemap":"exclude:health check"}}
]}`

func TestUncovered(t *testing.T) {
	m, err := parseManifest([]byte(sampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	miss := uncovered(m)
	// Only /browse is an indexable GET with no disposition. Everything else is
	// either disposed (book, login, health), a write, auth-gated, or skipped.
	if len(miss) != 1 {
		t.Fatalf("uncovered = %d, want 1: %+v", len(miss), miss)
	}
	if miss[0].Pattern != "/browse" {
		t.Errorf("uncovered route = %q, want /browse", miss[0].Pattern)
	}
}

func TestIndexableClassification(t *testing.T) {
	cases := []struct {
		name string
		r    route
		want bool
	}{
		{"plain GET", route{Method: "GET", Effect: "read_only"}, true},
		{"methodless read", route{Method: "", Effect: "read_only"}, true},
		{"mutating", route{Method: "GET", Effect: "mutating"}, false},
		{"post", route{Method: "POST", Effect: "read_only"}, false},
		{"auth", route{Method: "GET", Effect: "read_only", AuthRequired: true}, false},
		{"skipped", route{Method: "GET", Effect: "read_only", Skip: "x"}, false},
		{"head", route{Method: "HEAD", Effect: "read_only"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.indexable(); got != c.want {
				t.Errorf("indexable() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDispositionWhitespace(t *testing.T) {
	// A whitespace-only label counts as no disposition.
	r := route{Method: "GET", Effect: "read_only", Labels: map[string]string{"sitemap": "   "}}
	if r.disposition() != "" {
		t.Errorf("whitespace disposition = %q, want empty", r.disposition())
	}
	if !r.indexable() {
		t.Fatal("route should be indexable")
	}
	m := manifest{Routes: []route{r}}
	if len(uncovered(m)) != 1 {
		t.Error("whitespace-labelled route should be uncovered")
	}
}

func TestRunModes(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/good.json"
	bad := dir + "/bad.json"
	writeFile(t, good, `{"routes":[{"method":"GET","pattern":"/x","effect":"read_only","labels":{"sitemap":"main"}}]}`)
	writeFile(t, bad, `{"routes":[{"method":"GET","pattern":"/x","effect":"read_only"}]}`)

	if err := run([]string{"--manifest", good}); err != nil {
		t.Errorf("good manifest should pass: %v", err)
	}
	if err := run([]string{"--manifest", bad, "--mode", "fail"}); err == nil {
		t.Error("bad manifest in fail mode should error")
	}
	if err := run([]string{"--manifest", bad, "--mode", "warn"}); err != nil {
		t.Errorf("bad manifest in warn mode should not error: %v", err)
	}
	if err := run([]string{"--mode", "fail"}); err == nil {
		t.Error("missing --manifest should error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
