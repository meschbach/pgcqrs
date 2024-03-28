package systest

import (
	"context"
	"encoding/json"
	"github.com/go-faker/faker/v4"
	"github.com/meschbach/go-junk-bucket/pkg/stdgrpc/buffernet"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type cmdService struct {
	ipc.UnimplementedCommandServer
	transport v1.Transport
}

func (c *cmdService) CreateStream(ctx context.Context, in *ipc.CreateStreamIn) (*ipc.CreateStreamOut, error) {
	if in == nil || in.Target == nil {
		return nil, status.Error(codes.InvalidArgument, "nil")
	}
	//todo: verify this does not exist yet
	err := c.transport.EnsureStream(ctx, in.Target.Domain, in.Target.Stream)
	if err != nil {
		return nil, err
	}
	out := &ipc.CreateStreamOut{Existed: false}
	return out, err
}

func (c *cmdService) Submit(ctx context.Context, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
	result, err := c.transport.Submit(ctx, in.Events.Domain, in.Events.Stream, in.Kind, in.Body)
	if err != nil {
		return nil, err
	}
	out := &ipc.SubmitOut{
		Id:    result.ID,
		State: &ipc.Consistency{After: result.ID},
	}
	return out, nil
}

type queryService struct {
	ipc.UnimplementedQueryServer
	transport v1.Transport
}

func (q *queryService) Get(ctx context.Context, in *ipc.GetIn) (*ipc.GetOut, error) {
	var body json.RawMessage
	err := q.transport.GetEvent(ctx, in.Events.Domain, in.Events.Stream, in.Id, &body)
	if err != nil {
		return nil, err
	}
	out := &ipc.GetOut{
		Payload: body,
	}
	return out, nil
}

func TestV2System(t *testing.T) {
	ctx, done := context.WithCancel(context.Background())
	t.Cleanup(done)

	transport := v1.NewMemoryTransport()
	cmds := &cmdService{transport: transport}
	queryView := &queryService{transport: transport}

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
