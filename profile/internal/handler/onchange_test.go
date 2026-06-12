package handler

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// withOnChange installs an OnChange hook recording the keys it was called
// with.
func withOnChange(keys *[]string) func(*Deps) {
	return func(d *Deps) {
		d.OnChange = func(_ context.Context, userKey string) {
			*keys = append(*keys, userKey)
		}
	}
}

func TestOnChangeFiresOncePerSuccessfulSave(t *testing.T) {
	fs := newFakeStore()
	var keys []string
	h := newServer(t, fs, alice, withOnChange(&keys))

	// A save that also claims a handle (two store writes) fires once.
	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
		"display_name": {"Alice"}, "handle": {"alice"}, "is_public": {"on"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: got %d", rec.Code)
	}
	if len(keys) != 1 || keys[0] != "u1" {
		t.Errorf("after save: hook calls = %v, want [u1]", keys)
	}
}

func TestOnChangeNotFiredOnValidationFailure(t *testing.T) {
	fs := newFakeStore()
	var keys []string
	h := newServer(t, fs, alice, withOnChange(&keys))

	rec := do(t, h, http.MethodPost, basePath+"/edit", map[string][]string{
		"display_name": {"Alice"}, "handle": {"x"}, // too short
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422", rec.Code)
	}
	if len(keys) != 0 {
		t.Errorf("hook fired on validation failure: %v", keys)
	}
}

func TestOnChangeFiresOnAvatarWrites(t *testing.T) {
	fs := newFakeStore()
	var keys []string
	h := newServer(t, fs, alice, withOnChange(&keys), withIngest(
		func(_ context.Context, _ string, r io.Reader) (string, error) {
			if _, err := io.ReadAll(r); err != nil {
				return "", err
			}
			return "/media/avatar/x.webp", nil
		}))

	if rec := postAvatar(t, h, nil, []byte("img")); rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: got %d", rec.Code)
	}
	if rec := postAvatar(t, h, map[string]string{"action": "remove"}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove: got %d", rec.Code)
	}
	if len(keys) != 2 || keys[0] != "u1" || keys[1] != "u1" {
		t.Errorf("hook calls = %v, want [u1 u1]", keys)
	}
}
