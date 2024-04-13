package systest

import (
	"context"
	"github.com/go-faker/faker/v4"
	"github.com/meschbach/go-junk-bucket/pkg/stdgrpc/buffernet"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/service"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"testing"
)

func TestV2System(t *testing.T) {
	ctx, done := context.WithCancel(context.Background())
	t.Cleanup(done)

	transport := v1.NewMemoryTransport()
	cmds := service.ProxyingCommandService(transport)
	queryView := service.ProxyQueryService(transport)

	server := grpc.NewServer()
	ipc.RegisterCommandServer(server, cmds)
	ipc.RegisterQueryServer(server, queryView)

	grpcTransport := buffernet.New()
	_, onDoneListening := grpcTransport.ListenAsync(server)
	t.Cleanup(onDoneListening)

	wireClient, err := grpcTransport.Connect(ctx)
	require.NoError(t, err)
	commandClient := ipc.NewCommandClient(wireClient)
	queryClient := ipc.NewQueryClient(wireClient)

	t.Run("Able to create and find stream", func(t *testing.T) {
		domainName := faker.Word()
		streamName := faker.Word()
		kindeName := faker.Word()
		out, err := commandClient.CreateStream(ctx, &ipc.CreateStreamIn{Target: &ipc.DomainStream{
			Domain: domainName,
			Stream: streamName,
		}})
		require.NoError(t, err)
		assert.NotNil(t, out)
		submitOut, err := commandClient.Submit(ctx, &ipc.SubmitIn{
			Events: &ipc.DomainStream{
				Domain: domainName,
				Stream: streamName,
			},
			Kind: kindeName,
			Body: []byte("true"),
		})
		require.NoError(t, err)
		assert.NotNil(t, submitOut.Id)

		getOut, err := queryClient.Get(ctx, &ipc.GetIn{
			Events: &ipc.DomainStream{
				Domain: domainName,
				Stream: streamName,
			},
			Id: submitOut.Id,
		})
		require.NoError(t, err)
		assert.NotNil(t, getOut.Payload)
	})
}
