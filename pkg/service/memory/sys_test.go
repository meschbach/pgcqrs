package memory

import (
	"context"
	"io"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/meschbach/pgcqrs/pkg/junk/testgrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestSystem(t *testing.T) {
	t.Parallel()
	ctx, done := context.WithCancel(context.Background())
	t.Cleanup(done)

	client := testgrpc.InternalGRPConnection(t, ctx, func(server *grpc.Server) {
		cmdService, queryService := New()
		ipc.RegisterCommandServer(server, cmdService)
		ipc.RegisterQueryServer(server, queryService)
	})
	commands := ipc.NewCommandClient(client)
	queries := ipc.NewQueryClient(client)

	domainStream := &ipc.DomainStream{Domain: faker.Name(), Stream: faker.Name()}

	t.Run("When creating a new stream", func(t *testing.T) {
		t.Parallel()
		createStreamOut, err := commands.CreateStream(ctx, &ipc.CreateStreamIn{Target: domainStream})
		require.NoError(t, err)
		assert.False(t, createStreamOut.Existed, "stream did not exist")

		t.Run("And creating the same stream again", func(t *testing.T) {
			createStreamOut, err := commands.CreateStream(ctx, &ipc.CreateStreamIn{Target: domainStream})
			require.NoError(t, err)
			assert.True(t, createStreamOut.Existed, "stream did not exist")
		})

		t.Run("And there are no events", func(t *testing.T) {
			out, err := queries.Query(ctx, &ipc.QueryIn{
				Events: domainStream,
				OnEach: &ipc.OnEachEvent{
					Op: 1,
				},
			})
			require.NoError(t, err)
			for {
				record, err := out.Recv()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				assert.Fail(t, "received record", record)
			}
		})

		t.Run("And adding an event", func(t *testing.T) {
			exampleBody := []byte("{\"test\":1}")
			firstSubmit, err := commands.Submit(ctx, &ipc.SubmitIn{
				Events: domainStream,
				Kind:   "test/1",
				Body:   exampleBody,
			})
			require.NoError(t, err)

			t.Run("And querying for all events from the target stream", func(t *testing.T) {
				op := int64(faking.RandIntRange(0, 1000) + faking.RandIntRange(0, 1000))
				results, err := queries.Query(ctx, &ipc.QueryIn{
					Events: domainStream,
					OnEach: &ipc.OnEachEvent{
						Op: op,
					},
				})
				require.NoError(t, err)
				var found []*ipc.QueryOut
				for {
					record, err := results.Recv()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					found = append(found, record)
				}
				if assert.Len(t, found, 1) {
					assert.Equal(t, op, found[0].Op)
					assert.Equal(t, firstSubmit.Id, *found[0].Id)
					assert.Equal(t, exampleBody, found[0].Body)
				}
			})
		})

	})

}
