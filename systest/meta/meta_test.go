package meta

import (
	"context"
	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"testing"
)

var tracer = otel.Tracer("systest.meta")

func tracedTest(t *testing.T, parent context.Context, name string, test func(t *testing.T, ctx context.Context)) {
	t.Run(name, func(t *testing.T) {
		ctx, _ := tracer.Start(parent, t.Name())
		test(t, ctx)
	})
}

func TestMeta(t *testing.T) {
	t.Run("Given a system", func(t *testing.T) {
		_, ctx, sys := setupHarnessT(t)

		t.Run("When creating a new stream", func(t *testing.T) {
			domain := faker.FirstName()
			stream := faker.FirstName()

			_, err := sys.Stream(ctx, domain, stream)
			require.NoError(t, err)

			tracedTest(t, ctx, "Then application is listable", func(t *testing.T, ctx context.Context) {
				domains, err := sys.ListDomains(ctx)
				require.NoError(t, err)
				assert.Contains(t, domains, domain)
			})

			t.Run("Then is listable under the application", func(t *testing.T) {
				t.Skip("TODO")
			})
		})
	})
}
