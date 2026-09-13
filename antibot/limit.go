package antibot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"time"

	"github.com/infodancer/web/antibot/db"
)

// A Limit allows Max submissions per Window. The zero value means "use the
// default"; NoLimit disables the limit.
type Limit struct {
	Max    int
	Window time.Duration
}

// NoLimit disables a limit.
var NoLimit = Limit{Max: -1}

// Key kinds, as stored in antibot_counters.key_kind.
const (
	kindIP    = "ip"
	kindEmail = "email"
)

// keyDomain separates the limiter's key hashes from token signatures.
const keyDomain = "antibot/key/v1\x00"

func orLimit(v, def Limit) Limit {
	if v == (Limit{}) {
		return def
	}
	return v
}

// count records one submission against the key's current window. done reports
// that the check is over: the limit was exceeded, or the store failed. A store
// failure fails closed -- the verdict is a rejection and the error is returned.
func (g *Guard) count(ctx context.Context, form, ip, kind, value string, l Limit) (v Verdict, done bool, err error) {
	if l.Max < 0 {
		return Verdict{}, false, nil
	}
	start := windowStart(g.now(), l.Window)
	hits, err := g.store.Hit(ctx, db.HitParams{
		Form:        form,
		KeyKind:     kind,
		KeyHash:     g.keyHash(kind, value),
		WindowStart: start,
		ExpiresAt:   start.Add(l.Window),
	})
	if err != nil {
		return Verdict{Reason: ReasonUnavailable}, true, fmt.Errorf("antibot: count %s submission for %s: %w", kind, form, err)
	}
	if int(hits) > l.Max {
		return g.reject(ctx, form, ip, ReasonRateLimited, false), true, nil
	}
	return Verdict{}, false, nil
}

// keyHash is the stored form of a limiter key: an HMAC, so the table holds no
// address or email in recoverable form, keyed by the host's secret so the
// hashes cannot be brute-forced back to IPv4 addresses from a dump.
func (g *Guard) keyHash(kind, value string) []byte {
	mac := hmac.New(sha256.New, g.secret)
	mac.Write([]byte(keyDomain + kind + "\x00" + value))
	return mac.Sum(nil)
}

// ipKey is the value an IP is counted under. An IPv6 address is reduced to its
// /64: a subscriber is usually handed the whole prefix, so counting the full
// address would give them 2^64 separate budgets.
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

// windowStart aligns t to the start of its fixed window, counted from the Unix
// epoch, so every process computes the same window for the same instant.
func windowStart(t time.Time, window time.Duration) time.Time {
	secs := int64(window / time.Second)
	if secs <= 0 {
		secs = 1
	}
	u := t.Unix()
	return time.Unix(u-u%secs, 0).UTC()
}
