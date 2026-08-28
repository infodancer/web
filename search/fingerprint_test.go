package search

import (
	"errors"
	"testing"

	embedding "github.com/matthewjhunter/go-embedding"
)

// An unchanged fingerprint neither clears vectors nor errors.
func TestReconcileFingerprint_Unchanged(t *testing.T) {
	fp := embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "abc123"}

	got, clear, err := reconcileFingerprint(fp, fp, false)
	if err != nil {
		t.Fatalf("reconcileFingerprint: %v", err)
	}
	if clear {
		t.Error("an unchanged fingerprint must not clear the stored vectors")
	}
	if got != fp {
		t.Errorf("persisted %+v, want %+v", got, fp)
	}
}

// A corpus written before recipes were recorded adopts the current one and is
// checked from the next run. Forcing a re-embed of every existing corpus at
// upgrade would be worse than catching one change later.
func TestReconcileFingerprint_AdoptsAnUnrecordedRecipe(t *testing.T) {
	stored := embedding.Fingerprint{Model: testModel, Dim: 768}
	current := embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "abc123"}

	got, clear, err := reconcileFingerprint(stored, current, false)
	if err != nil {
		t.Fatalf("reconcileFingerprint: %v", err)
	}
	if clear {
		t.Error("adopting an unrecorded recipe must not clear the stored vectors")
	}
	if got.Recipe != "abc123" {
		t.Errorf("persisted recipe %q, want the current one adopted", got.Recipe)
	}
}

// A recipe change follows a deliberate edit, and the vectors it produced can no
// longer be mixed with what queries now produce. It clears without asking,
// because clearing is the whole remedy and a backfill rebuilds.
func TestReconcileFingerprint_RecipeChangeClearsWithoutConsent(t *testing.T) {
	stored := embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "old"}
	current := embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "new"}

	got, clear, err := reconcileFingerprint(stored, current, false)
	if err != nil {
		t.Fatalf("a recipe change must be resolvable without ReembedOnMismatch, got %v", err)
	}
	if !clear {
		t.Error("a recipe change must clear the stored vectors")
	}
	if got != current {
		t.Errorf("persisted %+v, want the current fingerprint %+v", got, current)
	}
}

// A model or dimension change means something moved underneath the deployment.
// Dropping the corpus automatically would hide the mistake, so it refuses.
func TestReconcileFingerprint_ModelChangeNeedsConsent(t *testing.T) {
	cases := map[string]struct{ stored, current embedding.Fingerprint }{
		"model swapped": {
			stored:  embedding.Fingerprint{Model: "old-model", Dim: 768, Recipe: "r"},
			current: embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "r"},
		},
		"dimension changed": {
			stored:  embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "r"},
			current: embedding.Fingerprint{Model: testModel, Dim: 1024, Recipe: "r"},
		},
		"model and recipe both moved": {
			stored:  embedding.Fingerprint{Model: "old-model", Dim: 768, Recipe: "old"},
			current: embedding.Fingerprint{Model: testModel, Dim: 768, Recipe: "new"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, clear, err := reconcileFingerprint(tc.stored, tc.current, false)
			if err == nil {
				t.Fatal("want a refusal without ReembedOnMismatch")
			}
			if clear {
				t.Error("a refused run must not clear anything")
			}
			var mismatch *embedding.MismatchError
			if !errors.As(err, &mismatch) {
				t.Errorf("error does not carry the fingerprints: %v", err)
			}

			// With consent, the same change rebuilds.
			got, clear, err := reconcileFingerprint(tc.stored, tc.current, true)
			if err != nil {
				t.Fatalf("with ReembedOnMismatch: %v", err)
			}
			if !clear {
				t.Error("with ReembedOnMismatch the stored vectors must be dropped")
			}
			if got != tc.current {
				t.Errorf("persisted %+v, want %+v", got, tc.current)
			}
		})
	}
}

// The recipe must move on exactly the things that relocate a vector, and stay
// put on the things that do not -- a signal that fires on every tuning tweak is
// one operators learn to ignore.
func TestDocumentRecipe(t *testing.T) {
	base := documentRecipe(testModel, 512)

	if got := documentRecipe(testModel, 512); got != base {
		t.Errorf("recipe is not stable across calls: %q then %q", base, got)
	}
	if got := documentRecipe(testModel, 256); got == base {
		t.Error("a changed chunk target must move the recipe")
	}
	if got := documentRecipe("embeddinggemma", 512); got == base {
		t.Error("a different model's prefixes must move the recipe")
	}
	// Packaging names the same weights. A serving-side rename must not force a
	// re-embed of the whole corpus.
	if got := documentRecipe(testModel+":latest", 512); got != base {
		t.Errorf("an Ollama tag moved the recipe: %q vs %q", got, base)
	}
	if got := documentRecipe(testModel+"-GGUF", 512); got != base {
		t.Errorf("a -GGUF suffix moved the recipe: %q vs %q", got, base)
	}
}
