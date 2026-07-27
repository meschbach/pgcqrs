//go:build testcontainers_pg

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var sharedPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) //nolint:forbidigo // TestMain has no parent context
	pool, cleanup, err := startPostgresContainer(ctx)
	if err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "failed to start testcontainers Postgres: %s\n", err)
		os.Exit(1)
	}
	sharedPool = pool

	code := m.Run()
	cancel()
	cleanup()
	os.Exit(code)
}

func startPostgresContainer(ctx context.Context) (*pgxpool.Pool, func(), error) {
	pg, err := postgres.Run(ctx,
		"postgres:18",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("starting postgres container: %w", err)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, fmt.Errorf("getting connection string: %w", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to pool: %w", err)
	}

	if err := applyMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("applying migrations: %w", err)
	}

	cleanup := func() {
		pool.Close()
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 10*time.Second) //nolint:forbidigo // cleanup has no parent context
		defer terminateCancel()
		if err := pg.Terminate(terminateCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to terminate postgres container: %s\n", err)
		}
	}

	return pool, cleanup, nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrationDir := filepath.Join("..", "..", "..", "migrations", "primary")

	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		return fmt.Errorf("reading migration dir %s: %w", migrationDir, err)
	}

	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, name := range upFiles {
		path := filepath.Join(migrationDir, name)
		//nolint:gosec // path is derived from migrations directory, not user input
		sql, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("executing %s: %w", name, err)
		}
	}

	return nil
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if sharedPool == nil {
		t.Skip("testcontainers Postgres not available")
	}
	return sharedPool
}

func createTestStream(ctx context.Context, t *testing.T, pool *pgxpool.Pool, app, stream string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO events_stream (app, stream)
		VALUES ($1, $2)
		ON CONFLICT (app, stream) DO NOTHING`, app, stream)
	require.NoError(t, err)
}

func resolveKindID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, kind string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(ctx, `
		INSERT INTO events_kind (kind) VALUES ($1)
		ON CONFLICT (kind) DO UPDATE SET kind = EXCLUDED.kind
		RETURNING id`, kind).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertEvent(ctx context.Context, t *testing.T, pool *pgxpool.Pool, app, stream, kind string, event interface{}) int64 {
	t.Helper()
	createTestStream(ctx, t, pool, app, stream)

	var streamID int64
	err := pool.QueryRow(ctx,
		`SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, app, stream,
	).Scan(&streamID)
	require.NoError(t, err)

	kindID := resolveKindID(ctx, t, pool, kind)

	payload, err := json.Marshal(event)
	require.NoError(t, err)

	var eventID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO events (stream_id, when_occurred, kind_id, event)
		VALUES ($1, now(), $2, $3)
		RETURNING id`, streamID, kindID, payload,
	).Scan(&eventID)
	require.NoError(t, err)
	return eventID
}
