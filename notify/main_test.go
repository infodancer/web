//go:build !integration

package notify

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the unit suite under goleak so a leaked emitter worker fails
// the build. The integration suite has its own TestMain (testcontainers).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
