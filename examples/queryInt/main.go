package main

import (
	"context"
	"fmt"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"os"
	"strconv"
	"time"
)

const app = "example.query"
const stream = "match-int-"

// Event is an example document for a subset match.
type Event struct {
	Value int `json:"id"`
}

// main is an example showing how to match a document via a document subset match.
func main() {
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()

	//
	// Fixtures for this test
	//
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	//
	// Configure a connection with the target system.
	//
	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}

	stream := sys.MustStream(ctx, app, streamName)

	//
	// Given seed data in the data store
	//
	kinds := faking.NewUniqueWords()
	values := faking.NewUniqueInts()
	target := values.Next()

	kind1 := kinds.Next()
	kind2 := kinds.Next()
	kind3 := kinds.Next()
	stream.MustSubmit(ctx, kind1, &Event{Value: values.Next()})
	expectedResult := stream.MustSubmit(ctx, kind2, &Event{Value: target})
	stream.MustSubmit(ctx, kind2, &Event{Value: values.Next()})
	stream.MustSubmit(ctx, kind3, &Event{Value: values.Next()})
	stream.MustSubmit(ctx, kind3, &Event{Value: target})

	//
	// When we query for a matching document
	//
	query := stream.Query()
	query.WithKind(kind2).Match(Event{Value: target})
	result, err := query.Perform(ctx)
	if err != nil {
		panic(err)
	}

	//
	// Then we receive teh expected event
	//
	envelopes := result.Envelopes()
	fmt.Printf("Received %#v\n", result.Envelopes())
	if len(envelopes) != 1 {
		fmt.Printf("FAILED -- expected a single envelope got %d\n", len(envelopes))
		os.Exit(-1)
	}
	fmt.Printf("Received %#v\n", envelopes[0])
	if envelopes[0].ID == expectedResult.ID {
		fmt.Println("Success")
	} else {
		fmt.Println("Failure")
	}
}
