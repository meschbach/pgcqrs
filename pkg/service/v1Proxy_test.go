package service

import (
	"context"
	"github.com/go-faker/faker/v4"
	"github.com/meschbach/go-junk-bucket/pkg/stdgrpc/buffernet"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"io"
	"testing"
)

var trueValue = true
var truePointer = &trueValue

func TestV2SystemProxy(t *testing.T) {
	ctx, done := context.WithCancel(context.Background())
	t.Cleanup(done)

	transport := v1.NewMemoryTransport()
	cmds := ProxyingCommandService(transport)
	queryView := ProxyQueryService(transport)

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

		domainStream := &ipc.DomainStream{
			Domain: domainName,
			Stream: streamName,
		}

		out, err := commandClient.CreateStream(ctx, &ipc.CreateStreamIn{Target: domainStream})
		require.NoError(t, err)
		assert.NotNil(t, out)
		submitOut, err := commandClient.Submit(ctx, &ipc.SubmitIn{
			Events: domainStream,
			Kind:   kindeName,
			Body:   []byte("true"),
		})
		require.NoError(t, err)
		assert.NotNil(t, submitOut.Id)

		t.Run("When getting by ID", func(t *testing.T) {
			getOut, err := queryClient.Get(ctx, &ipc.GetIn{
				Events: domainStream,
				Id:     submitOut.Id,
			})
			require.NoError(t, err)
			if assert.NotNil(t, getOut.Payload, "then has a payload") {
				assert.Equal(t, "true", string(getOut.Payload))
			}
		})

		t.Run("When querying", func(t *testing.T) {
			callbackOp := int64(94)
			queryResult, err := queryClient.Query(ctx, &ipc.QueryIn{
				Events: domainStream,
				OnKind: []*ipc.OnKindClause{
					{
						Kind:  kindeName,
						AllOp: &callbackOp,
						AllOpConfig: &ipc.ResultInclude{
							Envelope: truePointer,
							Body:     truePointer,
						},
					},
				},
			})
			require.NoError(t, err)
			opt, err := queryResult.Recv()
			require.NoError(t, err)
			if assert.NotNil(t, opt) {
				assert.Equal(t, callbackOp, opt.Op, "slot is called back")
			}
			opt2, err := queryResult.Recv()
			assert.ErrorIs(t, err, io.EOF, "expected end of stream")
			assert.Nil(t, opt2)
		})
	})
}
