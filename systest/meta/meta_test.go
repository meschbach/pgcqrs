package meta

import (
	"context"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("systest.meta")

func tracedTest(parent context.Context, t *testing.T, name string, test func(t *testing.T, ctx context.Context)) {
	t.Run(name, func(t *testing.T) {
		ctx, _ := tracer.Start(parent, t.Name())
		test(t, ctx)
	})
}

func TestMeta(t *testing.T) {
	t.Parallel()
	t.Run("Given a system", func(t *testing.T) {
		t.Parallel()
		_, ctx, sys := setupHarnessT(t)

		t.Run("When creating a new stream", func(t *testing.T) {
			t.Parallel()
			domain := faker.FirstName()
			stream := faker.FirstName()

			_, err := sys.Stream(ctx, domain, stream)
			require.NoError(t, err)

			tracedTest(ctx, t, "Then application is listable", func(t *testing.T, ctx context.Context) {
				domains, err := sys.ListDomains(ctx)
				require.NoError(t, err)
				assert.Contains(t, domains, domain)
			})
		})
	})
}
