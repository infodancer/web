//go:build !integration

package media

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the unit suite under goleak so a stray goroutine fails the
// build. The integration suite has its own TestMain (testcontainers).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
