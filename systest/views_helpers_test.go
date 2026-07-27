package systest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	"github.com/stretchr/testify/require"
)

// requireEntityValue fetches an entity, asserts StatusOK + not-nil, unmarshals
// the JSON value into T, and returns it for further assertion.
//
//nolint:revive // test helpers put t first, context second
func requireEntityValue[T any](t *testing.T, ctx context.Context, client *views.Client, kind string, key views.Key, version int64, timeout ...time.Duration) T {
	t.Helper()
	d := 500 * time.Millisecond
	if len(timeout) > 0 {
		d = timeout[0]
	}
	entity, result, err := client.Get(ctx, kind, key, views.UntilVersion(version, d))
	require.NoError(t, err)
	require.Equal(t, views.StatusOK, result.Status, "expected entity at version (ok), got %s", result.Status.String())
	require.NotNil(t, entity)

	var value T
	err = json.Unmarshal(entity.Value, &value)
	require.NoError(t, err)
	return value
}
