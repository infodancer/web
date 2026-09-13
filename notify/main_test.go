package notify

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the whole suite under goleak so a leaked emitter worker fails
// the build. The database tests take their own databases from pgtest, which
// closes each pool when its test ends, so they run under the same check.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
