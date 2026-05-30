package handler

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSubsiteSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"plain", "brandon-sanderson", nil},
		{"alphanum", "honor1", nil},
		{"single-char", "x", nil},
		{"max-length", strings.Repeat("a", 64), nil},
		{"empty", "", errSlugBadFormat},
		{"too-long", strings.Repeat("a", 65), errSlugBadFormat},
		{"uppercase", "Brandon", errSlugBadFormat},
		{"underscore", "wheel_of_time", errSlugBadFormat},
		{"leading-hyphen", "-leading", errSlugBadFormat},
		{"trailing-hyphen", "trailing-", errSlugBadFormat},
		{"double-hyphen", "foo--bar", errSlugBadFormat},
		{"space", "wheel of time", errSlugBadFormat},
		{"dot-ok-for-sitemap-xml-but-still-rejected-here", "site.map", errSlugBadFormat},

		// Reserved subsite slugs — must reject every literal top-level route.
		{"reserved-q", "q", errSlugReserved},
		{"reserved-tags", "tags", errSlugReserved},
		{"reserved-search", "search", errSlugReserved},
		{"reserved-admin", "admin", errSlugReserved},
		{"reserved-static", "static", errSlugReserved},
		{"reserved-health", "health", errSlugReserved},
		{"reserved-a", "a", errSlugReserved},
		{"reserved-comments", "comments", errSlugReserved},
		{"reserved-sitemap", "sitemap", errSlugReserved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateSubsiteSlug(tc.in)
			if !errors.Is(got, tc.want) {
				t.Errorf("validateSubsiteSlug(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateTagSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"plain", "wheel-of-time", nil},
		{"single-char", "x", nil},
		{"alphanum", "d20", nil},
		{"empty", "", errSlugBadFormat},
		{"uppercase", "Stormlight", errSlugBadFormat},
		{"underscore", "two_handed", errSlugBadFormat},

		// Reserved second-segment slugs — these would shadow literal
		// in-subsite routes if allowed.
		{"reserved-q", "q", errSlugReserved},
		{"reserved-tags", "tags", errSlugReserved},
		{"reserved-search", "search", errSlugReserved},
		{"reserved-ask", "ask", errSlugReserved},

		// Subsite-level reserved names like "admin" are fine as tag
		// names because the short form is /{subsite}/{tag} — the tag
		// position is second, not first.
		{"admin-is-fine-at-tag-position", "admin", nil},
		{"static-is-fine-at-tag-position", "static", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateTagSlug(tc.in)
			if !errors.Is(got, tc.want) {
				t.Errorf("validateTagSlug(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
