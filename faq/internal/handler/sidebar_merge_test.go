package handler

import (
	"testing"

	uiassets "github.com/infodancer/ui"
)

func sec(key string) uiassets.SidebarSection {
	return uiassets.SidebarSection{Key: key, Title: key}
}

func keys(d *uiassets.SidebarData) []string {
	if d == nil {
		return nil
	}
	out := make([]string, len(d.Sections))
	for i, s := range d.Sections {
		out[i] = s.Key
	}
	return out
}

func TestMergeSidebarTop(t *testing.T) {
	t.Run("nil extra returns base unchanged", func(t *testing.T) {
		base := &uiassets.SidebarData{Sections: []uiassets.SidebarSection{sec("a")}}
		if got := mergeSidebarTop(base, nil); got != base {
			t.Fatalf("nil extra should return base as-is")
		}
	})

	t.Run("nil base returns extra", func(t *testing.T) {
		extra := &uiassets.SidebarData{Sections: []uiassets.SidebarSection{sec("faq")}}
		if got := mergeSidebarTop(nil, extra); got != extra {
			t.Fatalf("nil base should return extra as-is")
		}
	})

	t.Run("extra sections go on top, base untouched", func(t *testing.T) {
		base := &uiassets.SidebarData{Sections: []uiassets.SidebarSection{sec("campaigns"), sec("tools")}}
		extra := &uiassets.SidebarData{Sections: []uiassets.SidebarSection{sec("faq")}}

		got := mergeSidebarTop(base, extra)
		want := []string{"faq", "campaigns", "tools"}
		gotKeys := keys(got)
		if len(gotKeys) != len(want) {
			t.Fatalf("got %v, want %v", gotKeys, want)
		}
		for i := range want {
			if gotKeys[i] != want[i] {
				t.Fatalf("got %v, want %v", gotKeys, want)
			}
		}

		// The inputs must not be mutated — the host's sidebar pointer is shared.
		if base.Sections[0].Key != "campaigns" || len(base.Sections) != 2 {
			t.Fatalf("base was mutated: %v", keys(base))
		}
		if len(extra.Sections) != 1 {
			t.Fatalf("extra was mutated: %v", keys(extra))
		}
	})
}
