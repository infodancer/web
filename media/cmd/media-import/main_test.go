package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePNG writes a small opaque PNG that media.Process accepts.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	f, err := os.Create(path) //nolint:gosec // test fixture writes to a t.TempDir path
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

func TestExpandPath(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), 16, 16)
	writePNG(t, filepath.Join(dir, "b.jpgnot"), 16, 16) // not an image extension
	if err := os.WriteFile(filepath.Join(dir, "c.jpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	t.Run("directory is non-recursive, image extensions only", func(t *testing.T) {
		got, err := expandPath(dir)
		if err != nil {
			t.Fatalf("expandPath(dir): %v", err)
		}
		want := []string{filepath.Join(dir, "a.png"), filepath.Join(dir, "c.jpg")}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("expandPath(dir) = %v, want %v", got, want)
		}
	})

	t.Run("plain file returns itself", func(t *testing.T) {
		p := filepath.Join(dir, "note.txt")
		got, err := expandPath(p)
		if err != nil {
			t.Fatalf("expandPath(file): %v", err)
		}
		if len(got) != 1 || got[0] != p {
			t.Errorf("expandPath(file) = %v, want [%s]", got, p)
		}
	})

	t.Run("glob matches files only, sorted", func(t *testing.T) {
		got, err := expandPath(filepath.Join(dir, "*.png"))
		if err != nil {
			t.Fatalf("expandPath(glob): %v", err)
		}
		if len(got) != 1 || got[0] != filepath.Join(dir, "a.png") {
			t.Errorf("expandPath(glob) = %v, want [a.png]", got)
		}
	})

	t.Run("missing path errors", func(t *testing.T) {
		if _, err := expandPath(filepath.Join(dir, "nope.png")); err == nil {
			t.Error("expandPath(missing) = nil error, want error")
		}
	})
}

func TestRun_DryRunText(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "img.png")
	writePNG(t, file, 24, 18)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--category", "cover", "--dry-run", file}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "(dry-run)") || !strings.Contains(out, file) {
		t.Errorf("stdout = %q, want a (dry-run) line for %s", out, file)
	}
	// 64-hex sha256 prefix on the line.
	if fields := strings.Fields(out); len(fields) < 1 || len(fields[0]) != 64 {
		t.Errorf("first field = %q, want 64-char hash", out)
	}
}

func TestRun_DryRunJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "img.png")
	writePNG(t, file, 24, 18)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--category", "cover", "--dry-run", "--json", file}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, stderr.String())
	}
	var rec jsonRecord
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rec); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if rec.Source != file || len(rec.Hash) != 64 || rec.Width != 24 || rec.Height != 18 {
		t.Errorf("record = %+v, want source=%s 64-hex hash 24x18", rec, file)
	}
	if rec.URL != "" {
		t.Errorf("dry-run URL = %q, want empty (URLs are minted only on write)", rec.URL)
	}
}

func TestRun_DryRunReportsBadImage(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--category", "cover", "--dry-run", bad}, &stdout, &stderr)
	if err == nil {
		t.Error("run with invalid image = nil error, want failure")
	}
	if !strings.Contains(stderr.String(), "ERROR") {
		t.Errorf("stderr = %q, want an ERROR line", stderr.String())
	}
}

func TestRun_FlagValidation(t *testing.T) {
	// A real image so cases meant to test a later check are not short-circuited
	// by source resolution failing on a missing file.
	img := filepath.Join(t.TempDir(), "img.png")
	writePNG(t, img, 16, 16)

	tests := []struct {
		name string
		args []string
	}{
		{"missing category", []string{"--dry-run", img}},
		{"bad blob backend", []string{"--category", "c", "--blob", "s3", img}},
		{"fs without dir", []string{"--category", "c", "--blob", "fs", img}},
		{"no sources", []string{"--category", "c", "--dry-run"}},
		{"real run without database-url", []string{"--category", "c", img}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(context.Background(), tt.args, &stdout, &stderr); err == nil {
				t.Errorf("run(%v) = nil error, want error", tt.args)
			}
		})
	}
}
