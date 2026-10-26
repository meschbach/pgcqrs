package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpsListenerConfigDefaultsAndEnvOverrides(t *testing.T) {
	// Intentionally sequential: environment mutation across parallel tests
	// would race; the paralleltest linter is suppressed above.
	cfg := &Config{}
	cfg.LoadDefaults()
	require.NotNil(t, cfg.OpsListener)
	assert.Equal(t, "127.0.0.1:9002", cfg.OpsListener.Address)
	require.NotNil(t, cfg.OpsListener.Enabled)
	assert.True(t, *cfg.OpsListener.Enabled)

	t.Setenv("PGCQRS_OPS_LISTENER_ADDRESS", "0.0.0.0:9003")
	t.Setenv("PGCQRS_OPS_LISTENER_ENABLED", "false")

	cfg2 := &Config{}
	cfg2.LoadDefaults()
	require.NotNil(t, cfg2.OpsListener)
	assert.Equal(t, "0.0.0.0:9003", cfg2.OpsListener.Address)
	require.NotNil(t, cfg2.OpsListener.Enabled)
	assert.False(t, *cfg2.OpsListener.Enabled)
}
