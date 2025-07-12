package main

import (
	"context"
	"fmt"
	"github.com/meschbach/go-junk-bucket/pkg"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"time"
)

const app = "example-simple"
const stream = "base"

type Event struct {
	First bool `json:"first"`
}

func main() {
	url := pkg.EnvOrDefault("PGCQRS_URL", "localhost:9001")

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	transport, err := v1.NewGRPCTransport(url)
	if err != nil {
		panic(err)
	}
	sys := v1.NewSystem(transport)
	stream := sys.MustStream(ctx, app, stream)
	reply := stream.MustSubmit(ctx, "general", &Event{First: true})

	var byID Event
	stream.MustGet(ctx, reply.ID, &byID)

	envelopes := stream.MustAll(ctx)

	fmt.Printf("Events: %d\n", len(envelopes))
	for _, envelope := range envelopes {
		var byMeta Event
		stream.MustGet(ctx, envelope.ID, &byMeta)
		fmt.Printf("\t%#v: %#v\n", envelope, byMeta)
	}
}
