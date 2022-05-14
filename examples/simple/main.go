package main

import (
	"context"
	"fmt"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"time"
)

const app = "example-simple"
const stream = "base"

type Event struct {
	First bool `json:"first"`
}

func main() {
	url := "http://localhost:9000"

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	sys := v1.NewSystem(v1.NewHttpTransport(url))
	stream, err := sys.Stream(ctx, app, stream)
	junk.Must(err)
	reply, err := stream.Submit(ctx, "general", &Event{First: true})
	junk.Must(err)

	var byID Event
	junk.Must(stream.Get(ctx, reply.ID, &byID))
	fmt.Printf("Event by ID %#v\n", byID)

	envelopes, err := stream.All(ctx)
	junk.Must(err)

	fmt.Printf("Events: %d\n", len(envelopes))
	for _, envelope := range envelopes {
		var byMeta Event
		junk.Must(stream.Get(ctx, envelope.ID, &byMeta))
		fmt.Printf("\t%#v: %#v\n", envelope, byMeta)
	}
}
