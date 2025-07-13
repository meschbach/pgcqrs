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
	//Word is teh matched field for our target.
	Word string `json:"word"`
}

// main defines a test program for verifying a v1 query is able to match a specific field on the target entity.
//
// * will create a new stream for each iteration, deconflicted by the current time as base36 encoded.
// * Creates events of several different kinds with both matching and non-matching events.
func main() {
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}
	stream := sys.MustStream(ctx, app, streamName)

	kind1 := faker.Word()
	target := faker.Word()
	kind2 := faker.Word()
	kind3 := faker.Word()
	stream.MustSubmit(ctx, kind1, &Event{Word: faker.Word()})
	stream.MustSubmit(ctx, kind2, &Event{Word: target})
	stream.MustSubmit(ctx, kind2, &Event{Word: faker.Word()})
	stream.MustSubmit(ctx, kind3, &Event{Word: faker.Word()})
	stream.MustSubmit(ctx, kind3, &Event{Word: target})

	query := stream.Query()
	query.WithKind(kind2).Eq("word", target)
	result, err := query.Perform(ctx)
	if err != nil {
		panic(err)
	}

	envelopes := result.Envelopes()
	fmt.Printf("%v\n", envelopes)

	result, err = query.Perform(ctx)
	if err != nil {
		panic(err)
	}
	if len(envelopes) != 1 {
		fmt.Printf("FAILED -- expected a single envelope got %d\n", len(envelopes))
		os.Exit(-1)
	}
}
