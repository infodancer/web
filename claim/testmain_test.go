package claim

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDSN is the connection string for the package's throwaway Postgres,
// started once in TestMain.
var testDSN string

// ContainerDSN exposes the container DSN to the external claim_test package.
//
// TestMain has to live in one package, and the internal tests need the DSN to
// drive generated queries directly, so it lives here. Declaring the accessor in
// a _test.go file keeps it out of the real API: it exists only in test builds.
func ContainerDSN() string { return testDSN }

// TestMain spins one testcontainers Postgres for the whole package, matching the
// suite convention (faq/media/notify/timeline) so CI needs no Postgres service
// container and runs on the self-hosted runners. Requires a working Docker
// daemon.
func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("claim_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("postgres container: %v", err)
	}
	testDSN, err = pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}

	code := m.Run()

	_ = pg.Terminate(ctx)
	os.Exit(code)
}
