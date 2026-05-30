package faq

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	uiassets "github.com/infodancer/ui"
)

// TestWithUIAssets_RoutesUIPaths verifies the static-FS wrapper that
// faq.New uses to expose infodancer/ui's tokens.css and base.css under
// the /static/ui/ path. The wrapper hits infodancer/ui's embedded FS
// for "ui/..." paths and faq's own static for everything else.
func TestWithUIAssets_RoutesUIPaths(t *testing.T) {
	faqOnly := fstest.MapFS{
		"faq.css":      &fstest.MapFile{Data: []byte("/* faq stylesheet */")},
		"unrelated.js": &fstest.MapFile{Data: []byte("// faq script")},
	}
	wrapped := withUIAssets(faqOnly)

	// faq's own file resolves to faq's FS.
	if got, err := fs.ReadFile(wrapped, "faq.css"); err != nil || string(got) != "/* faq stylesheet */" {
		t.Errorf("faq.css: got (%q, %v); want faq stylesheet", got, err)
	}

	// ui/ paths route into infodancer/ui's assets.
	tokens, err := fs.ReadFile(wrapped, "ui/css/tokens.css")
	if err != nil {
		t.Fatalf("ui/css/tokens.css: unexpected error: %v", err)
	}
	if len(tokens) == 0 {
		t.Errorf("ui/css/tokens.css: file is empty")
	}

	base, err := fs.ReadFile(wrapped, "ui/css/base.css")
	if err != nil {
		t.Fatalf("ui/css/base.css: unexpected error: %v", err)
	}
	if len(base) == 0 {
		t.Errorf("ui/css/base.css: file is empty")
	}

	// A missing ui-prefixed path returns an error, not silently faq.
	if _, err := fs.ReadFile(wrapped, "ui/nonexistent.txt"); err == nil {
		t.Errorf("ui/nonexistent.txt: expected error, got success")
	}
}

// TestWithUIAssets_ServesVendoredHTMX verifies the self-hosted htmx
// library resolves through the same ui-routed FS that backs the
// /static/ui CSS. The path is derived from the ui package's pinned
// version so a future htmx bump in ui keeps this test honest without an
// edit here. Guards the migration off the unpkg CDN.
func TestWithUIAssets_ServesVendoredHTMX(t *testing.T) {
	wrapped := withUIAssets(fstest.MapFS{})

	name := "ui/js/htmx-" + uiassets.HTMXVersion + ".min.js"
	js, err := fs.ReadFile(wrapped, name)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", name, err)
	}
	if len(js) == 0 {
		t.Errorf("%s: file is empty", name)
	}
}

// TestHTMXHead_IsSelfHostedWithSRI pins the contract the base template
// relies on: ui.HeadTags emits a self-hosted <script> under the module's
// own /static/ui path, carries a Subresource Integrity hash, and never
// reaches for a remote CDN. This is the anti-pattern the migration
// eliminated.
func TestHTMXHead_IsSelfHostedWithSRI(t *testing.T) {
	head := string(uiassets.HeadTags("/faq/static/ui"))

	if !strings.Contains(head, "/faq/static/ui/js/htmx-"+uiassets.HTMXVersion+".min.js") {
		t.Errorf("HeadTags missing self-hosted htmx src; got %q", head)
	}
	if !strings.Contains(head, "integrity=") {
		t.Errorf("HeadTags missing Subresource Integrity hash; got %q", head)
	}
	for _, bad := range []string{"http", "unpkg", "cdn", "jsdelivr"} {
		if strings.Contains(head, bad) {
			t.Errorf("HeadTags references remote source %q; got %q", bad, head)
		}
	}
}
