package service

import (
	"context"

	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type grpcConsumerPosition struct {
	ipc.UnimplementedConsumerPositionServer
	store *storage2.ConsumerStore
}

func (g *grpcConsumerPosition) SetPosition(ctx context.Context, in *ipc.SetPositionIn) (*ipc.SetPositionOut, error) {
	result, err := g.store.SetPosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.EventID)
	if err != nil {
		return &ipc.SetPositionOut{Ok: false, Error: err.Error()}, nil
	}
	var prevID int64
	if result.PreviousEventID != nil {
		prevID = *result.PreviousEventID
	}
	return &ipc.SetPositionOut{
		Ok:              true,
		CurrentEventID:  result.CurrentEventID,
		PreviousEventID: prevID,
	}, nil
}

func (g *grpcConsumerPosition) GetPosition(ctx context.Context, in *ipc.GetPositionIn) (*ipc.GetPositionOut, error) {
	eventID, found, err := g.store.GetPosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer)
	if err != nil {
		return nil, err
	}
	return &ipc.GetPositionOut{EventID: eventID, Found: found}, nil
}

func (g *grpcConsumerPosition) ListConsumers(ctx context.Context, in *ipc.ListConsumersIn) (*ipc.ListConsumersOut, error) {
	consumers, err := g.store.ListConsumers(ctx, in.Events.Domain, in.Events.Stream)
	if err != nil {
		return nil, err
	}
	return &ipc.ListConsumersOut{Consumers: consumers}, nil
}

func (g *grpcConsumerPosition) DeletePosition(ctx context.Context, in *ipc.DeletePositionIn) (*ipc.DeletePositionOut, error) {
	err := g.store.DeletePosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer)
	if err != nil {
		return nil, err
	}
	return &ipc.DeletePositionOut{Ok: true}, nil
}
