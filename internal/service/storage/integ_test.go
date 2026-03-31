package storage

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	_ "github.com/golang-migrate/migrate/v4/database/pgx"
)

// normalizeDatabaseURL adds the postgres:// scheme if missing.
// The PGCQRS_STORAGE_POSTGRES_URL environment variable may not include the scheme.
func normalizeDatabaseURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(rawURL, "postgres://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "://") {
		return "postgres" + rawURL
	}
	return "postgres://" + rawURL
}

// WithDatabaseConnection creates a PostgreSQL connection pool for testing
// using the TEMPLATE strategy for isolation.
func WithDatabaseConnection(t *testing.T) *pgxpool.Pool {
	t.Helper()
	rawURL := os.Getenv("PGCQRS_STORAGE_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("Skipping database test because -integration is not set")
	}

	mainURL := normalizeDatabaseURL(rawURL)

	if testing.Verbose() {
		fmt.Printf("Using Postgres template %q\n", mainURL)
	}

	// Connect to the test database
	testConfig, err := pgxpool.ParseConfig(mainURL)
	require.NoError(t, err)

	// Generate unique database name for this test
	// Format: test_<testname>_<timestamp>_<random>
	testName := strings.ReplaceAll(t.Name(), "/", "_") // Sanitize for DB name
	timestamp := time.Now().UnixNano()
	//nolint
	random := rand.Intn(1000000)
	var trimmedName string
	if len(testName) > 44 {
		trimmedName = testName[:44]
	} else {
		trimmedName = testName
	}
	dbName := fmt.Sprintf("test_%s_%06d_%06d", trimmedName, timestamp%10000, random)

	ctx := t.Context()

	// Connect to template database
	adminUserURL := "postgres://" + os.Getenv("PGCQRS_INTEG_POSTGRES_URL")
	adminConfig, err := pgconn.ParseConfig(adminUserURL)
	require.NoError(t, err)

	adminConn, err := pgconn.ConnectConfig(ctx, adminConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, adminConn.Close(ctx))
	})

	statement := fmt.Sprintf("CREATE DATABASE %q WITH OWNER %q TEMPLATE=%s IS_TEMPLATE=false", dbName, testConfig.ConnConfig.User, testConfig.ConnConfig.Database)
	if testing.Verbose() {
		fmt.Println(statement)
	}

	r := adminConn.Exec(ctx, statement)
	results, err := r.ReadAll()
	require.NoError(t, err)
	for _, r := range results {
		require.NoError(t, r.Err)
	}

	require.NoError(t, r.Close())
	dbCleanup := &cleanUpDatabase{
		databaseName: dbName,
		connection:   adminConn,
		t:            t,
	}
	t.Cleanup(dbCleanup.cleanup)

	//
	testConfig.ConnConfig.Database = dbName

	p, err := pgxpool.NewWithConfig(ctx, testConfig)
	require.NoError(t, err)

	waitForConnection(ctx, p)

	t.Cleanup(func() {
		p.Close()
	})

	return p
}

func waitForConnection(ctx context.Context, pool *pgxpool.Pool) {
	attempt := func() bool {
		connectionTimeout, done := context.WithTimeout(ctx, 100*time.Millisecond)
		defer done()

		_, err := pool.Exec(connectionTimeout, "SELECT 1")
		if err == nil {
			return true
		}
		fmt.Printf("Failed with %s, retrying shortly\n", err.Error())
		time.Sleep(500 * time.Millisecond)
		return false
	}
	for i := 0; i < 30; i++ {
		if attempt() {
			return
		}
	}
}

func createStreamForTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, domain, stream string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO events_stream (app, stream)
		VALUES ($1, $2)
		ON CONFLICT (app, stream) DO NOTHING`, domain, stream)
	require.NoError(t, err)
}

type cleanUpDatabase struct {
	databaseName string
	connection   *pgconn.PgConn
	t            *testing.T
}

func (c *cleanUpDatabase) cleanup() {
	statement := fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, c.databaseName)

	//nolint
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	d := c.connection.Exec(ctx, statement)
	require.NoError(c.t, d.Close())
}
