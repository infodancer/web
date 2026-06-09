package handler

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/infodancer/web/profile/store"
)

// Field limits. Bio is "a paragraph of markdown", not a blog post; links are
// a short list of profile pointers, not a directory.
const (
	handleMinLen   = 3
	handleMaxLen   = 32
	displayNameMax = 80
	bioMaxLen      = 2000
	linkLabelMax   = 40
	linkURLMax     = 200
	maxLinks       = 8
)

// handleRe matches a normalized handle: lowercase alphanumerics and interior
// hyphens, no leading/trailing hyphen. Length is enforced separately for a
// clearer error message.
var handleRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// builtinReserved are handles no user may claim: the module's own route
// words (so a profile URL can never shadow a route) plus names that imply
// authority or break URLs. Hosts extend the set via Config.ReservedHandles.
var builtinReserved = []string{
	// route words and likely future routes
	"edit", "avatar", "static", "card", "u", "api", "profile", "profiles",
	// authority
	"admin", "administrator", "root", "mod", "moderator", "staff", "system",
	"support", "help", "abuse", "security", "webmaster", "postmaster",
	// confusing or degenerate
	"me", "anonymous", "everyone", "nobody", "null", "undefined", "www",
}

// normalizeHandle lowercases and trims a submitted handle. Validation runs on
// the normalized form; storage and lookup always use it, which is what makes
// the plain UNIQUE constraint case-insensitive in practice.
func normalizeHandle(h string) string {
	return strings.ToLower(strings.TrimSpace(h))
}

// validateHandle checks a normalized handle against shape, length, and the
// reserved set. Returns a user-facing message, or "" when valid.
func validateHandle(h string, reserved map[string]bool) string {
	if len(h) < handleMinLen || len(h) > handleMaxLen {
		return fmt.Sprintf("handle must be %d-%d characters", handleMinLen, handleMaxLen)
	}
	if !handleRe.MatchString(h) {
		return "handle may contain only lowercase letters, digits, and interior hyphens"
	}
	if reserved[h] {
		return "that handle is reserved"
	}
	return ""
}

// validateDisplayName trims, strips control characters, and length-checks a
// display name. Returns the cleaned value and a user-facing message ("" when
// valid).
func validateDisplayName(name string) (string, string) {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if len(cleaned) > displayNameMax {
		return cleaned, fmt.Sprintf("display name must be at most %d characters", displayNameMax)
	}
	return cleaned, ""
}

// validateBio length-checks the markdown source. Sanitization happens at
// render time through the audited ui/markdown pipeline; the stored value is
// markdown text, never HTML.
func validateBio(bio string) (string, string) {
	bio = strings.TrimSpace(bio)
	if len(bio) > bioMaxLen {
		return bio, fmt.Sprintf("bio must be at most %d characters", bioMaxLen)
	}
	return bio, ""
}

// parseLinks zips the repeated link_label/link_url form fields into validated
// links. Rows with an empty URL are skipped (empty form rows are normal);
// a URL without a label uses its host as the label. Only absolute http/https
// URLs are accepted.
func parseLinks(labels, urls []string) ([]store.Link, []string) {
	var links []store.Link
	var errs []string
	for i, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if len(links) >= maxLinks {
			errs = append(errs, fmt.Sprintf("at most %d links allowed", maxLinks))
			break
		}
		if len(raw) > linkURLMax {
			errs = append(errs, fmt.Sprintf("link %d: URL must be at most %d characters", i+1, linkURLMax))
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Sprintf("link %d: must be an absolute http or https URL", i+1))
			continue
		}
		label := ""
		if i < len(labels) {
			label = strings.TrimSpace(labels[i])
		}
		if label == "" {
			label = u.Host
		}
		if len(label) > linkLabelMax {
			errs = append(errs, fmt.Sprintf("link %d: label must be at most %d characters", i+1, linkLabelMax))
			continue
		}
		links = append(links, store.Link{Label: label, URL: u.String()})
	}
	return links, errs
}
