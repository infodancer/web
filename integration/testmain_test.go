package integration

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDSN is the connection string for the suite's throwaway Postgres, started
// once in TestMain. The integration suite's whole point is that several
// components migrate into one shared database, so all tests connect to this one
// container through newTestPool; each test truncates its own tables.
var testDSN string

// TestMain spins one testcontainers Postgres for the suite, matching the suite
// convention (faq/media/notify) so CI needs no Postgres service container and
// runs on the self-hosted runners. Requires a working Docker daemon.
func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("web_test"),
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
