package memory

import (
	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type queryService struct {
	ipc.UnimplementedQueryServer
	core *core
}

//func (q *queryService) ListStreams(ctx context.Context, in *ipc.ListStreamsIn) (*ipc.ListStreamsOut, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (q *queryService) Get(ctx context.Context, in *ipc.GetIn) (*ipc.GetOut, error) {
//	//TODO implement me
//	panic("implement me")
//}

func (q *queryService) Query(in *ipc.QueryIn, server ipc.Query_QueryServer) error {
	stream, has := q.core.lookup(in.Events)
	if !has {
		return nil
	}
	for _, e := range stream.events {
		if err := server.Send(&ipc.QueryOut{
			Op:       in.OnEach.Op,
			Id:       &e.id,
			Envelope: nil,
			Body:     e.body,
		}); err != nil {
			return err
		}
	}
	return nil
}
