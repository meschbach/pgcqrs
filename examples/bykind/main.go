package main

import (
	"context"
	"fmt"
	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"os"
	"strconv"
	"time"
)

const app = "example.bykind"
const stream = "kindstream"

type Event struct {
	First bool `json:"first"`
}

// main defins an example client which validates v1 query behavior of locating all documents within a stream by ID.
//
// * will create a new stream for each iteration, deconflicted by the current time as base36 encoded.
// * Creates events of several different kinds to ensure we only find matching events.
func main() {
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	kind1 := faker.Name()
	kind2 := faker.Name()
	kind3 := faker.Name()
	kind4 := faker.Name()

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()

	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}

	stream := sys.MustStream(ctx, app, streamName)
	stream.MustSubmit(ctx, kind1, &Event{First: true})
	stream.MustSubmit(ctx, kind2, &Event{First: true})
	stream.MustSubmit(ctx, kind3, &Event{First: false})
	stream.MustSubmit(ctx, kind3, &Event{First: false})
	stream.MustSubmit(ctx, kind3, &Event{First: false})
	stream.MustSubmit(ctx, kind3, &Event{First: true})
	stream.MustSubmit(ctx, kind3, &Event{First: true})
	stream.MustSubmit(ctx, kind4, &Event{First: true})

	envelopes := stream.MustByKind(ctx, kind3)
	fmt.Printf("Create(%q): 5 = %#v ?\n", kind3, len(envelopes))
	if len(envelopes) != 5 {
		fmt.Printf("FAILED -- incorrect envelope count")
		os.Exit(-1)
	}
}
