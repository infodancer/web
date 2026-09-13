package antibot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// tokenDomain separates token signatures from the limiter's key hashes, which
// use the same secret: a value valid as one can never be valid as the other.
const tokenDomain = "antibot/token/v1\x00" //nolint:gosec // G101 false positive: a public domain-separation label; the secret is Config.Secret

// Token returns the timing token to embed in form as it is rendered:
// "<unix seconds>.<signature>". The signature covers the form name and the
// timestamp exactly as written, so neither can be changed without breaking it.
func (g *Guard) Token(form string) string {
	ts := strconv.FormatInt(g.now().Unix(), 10)
	return ts + "." + base64.RawURLEncoding.EncodeToString(g.tokenMAC(form, ts))
}

func (g *Guard) tokenMAC(form, ts string) []byte {
	mac := hmac.New(sha256.New, g.secret)
	mac.Write([]byte(tokenDomain + form + "\x00" + ts))
	return mac.Sum(nil)
}

// verifyToken returns why tok is not a valid token for form right now, or ""
// if it is. The signature is checked before the timing, so a forged timestamp
// is reported as invalid rather than as too fast or too old.
func (g *Guard) verifyToken(form, tok string) Reason {
	if tok == "" {
		return ReasonTokenMissing
	}
	ts, sig, ok := strings.Cut(tok, ".")
	if !ok || ts == "" || sig == "" {
		return ReasonTokenInvalid
	}
	issued, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ReasonTokenInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, g.tokenMAC(form, ts)) {
		return ReasonTokenInvalid
	}

	elapsed := g.now().Sub(time.Unix(issued, 0))
	switch {
	case elapsed < 0:
		// One server signs and checks, so this token was not issued here.
		return ReasonTokenInvalid
	case elapsed < g.minFill:
		return ReasonTooFast
	case elapsed > g.maxAge:
		return ReasonTokenExpired
	}
	return ""
}
