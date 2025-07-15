package main

import (
	"context"
	"fmt"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"os"
	"strconv"
	"time"
)

const app = "example.query2"
const stream = "match-doc-"

type Event struct {
	Word   *string `json:"word,omitempty"`
	Number *int    `json:"number,omitempty"`
}

type matchEventWord struct {
	Word string `json:"word"`
}

// main provides an example of matching a subset of documents via the query2 interface.
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
	words := faking.NewUniqueWords()
	targetWord := words.Next()
	targetWordPtr := &targetWord

	values := faking.NewUniqueInts()
	value1 := values.Next()
	value2 := values.Next()

	kind1 := kinds.Next()
	kind2 := kinds.Next()
	kind3 := kinds.Next()
	stream.MustSubmit(ctx, kind1, &Event{Word: words.NextPtr()})
	stream.MustSubmit(ctx, kind2, &Event{Word: targetWordPtr, Number: &value1})
	stream.MustSubmit(ctx, kind2, &Event{Word: targetWordPtr, Number: &value2})
	stream.MustSubmit(ctx, kind2, &Event{Word: words.NextPtr()})
	stream.MustSubmit(ctx, kind3, &Event{Word: words.NextPtr()})
	stream.MustSubmit(ctx, kind3, &Event{Word: targetWordPtr})

	//
	// When we query for a matching document
	//
	var matched []Event
	query := query2.NewQuery(stream)
	query.OnKind(kind2).Subset(matchEventWord{Word: targetWord}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		matched = append(matched, entity)
	}))
	if err := query.StreamBatch(ctx); err != nil {
		panic(err)
	}

	//
	// Then we find our events
	//
	if len(matched) != 2 {
		fmt.Printf("FAILED -- expected 2 matches got %d\n", len(matched))
		os.Exit(-1)
	}
	if matched[0].Number != nil && *matched[0].Number != value1 {
		fmt.Printf("FAILED -- event 0 has wrong match: %d\n%#v\n", matched[0].Number, matched)
		os.Exit(-1)
	}
	if matched[1].Number != nil && *matched[1].Number != value2 {
		fmt.Printf("FAILED -- event 1 has wrong match: %d\n%#v\n", matched[1].Number, matched)
		os.Exit(-1)
	}
	fmt.Printf("Success\n")
}
