package main

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

type Event struct {
	First bool `json:"first"`
}

func main() {
	// setting up go environment -- must complete within 2 seconds
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()

	// loading configuration from environment variables
	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}

	//Creates the stream
	exampleKind := "example" //used for the kind of event
	stream := sys.MustStream(ctx, "readme", "test")
	//submit events to be queried
	stream.MustSubmit(ctx, exampleKind, &Event{First: true})
	stream.MustSubmit(ctx, exampleKind, &Event{First: false})

	// prepare a query to find all events with First == true
	q := query2.NewQuery(stream)
	q.OnKind(exampleKind).Subset(Event{First: true}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		//will be called for each found event as the query is executing
		fmt.Printf("%#v\n", e)
	}))
	if err = q.StreamBatch(ctx); err != nil {
		panic(err)
	}
}
