package main

import (
	"context"
	"fmt"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"time"
)

const app = "example-simple"
const stream = "base"

type Event struct {
	First bool `json:"first"`
}

func main() {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()

	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}

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
