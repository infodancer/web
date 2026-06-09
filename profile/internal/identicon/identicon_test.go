package identicon

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	a := Generate("alice")
	b := Generate("alice")
	if !bytes.Equal(a, b) {
		t.Error("same seed produced different output")
	}
}

func TestGenerateDiffersBySeed(t *testing.T) {
	if bytes.Equal(Generate("alice"), Generate("bob")) {
		t.Error("different seeds produced identical output")
	}
}

func TestGenerateWellFormedXML(t *testing.T) {
	for _, seed := range []string{"alice", "", "x", strings.Repeat("long", 100), `<script>alert(1)</script>`} {
		out := Generate(seed)
		dec := xml.NewDecoder(bytes.NewReader(out))
		for {
			_, err := dec.Token()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("seed %q: malformed XML: %v\n%s", seed, err, out)
			}
		}
	}
}

func TestGenerateNeverEchoesSeed(t *testing.T) {
	seed := `"><script>alert(1)</script>`
	if bytes.Contains(Generate(seed), []byte("script")) {
		t.Error("seed content leaked into SVG output")
	}
}

func TestGenerateHasSymmetry(t *testing.T) {
	// The grid mirrors across the vertical axis: every cell rect at x=1 must
	// pair with one at x=5 on the same row (and x=2 with x=4).
	out := string(Generate("symmetry-check"))
	for _, pair := range [][2]string{{`x="1"`, `x="5"`}, {`x="2"`, `x="4"`}} {
		if strings.Count(out, pair[0]) != strings.Count(out, pair[1]) {
			t.Errorf("asymmetric output: %s count != %s count", pair[0], pair[1])
		}
	}
}
