package timeline_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDSN is the connection string for the package's throwaway Postgres,
// started once in TestMain. Tests requiring a database connect through newPool.
var testDSN string

// TestMain spins one testcontainers Postgres for the whole package, matching the
// suite convention (faq/media/notify) so CI needs no Postgres service container
// and runs on the self-hosted runners. Requires a working Docker daemon.
func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("timeline_test"),
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
