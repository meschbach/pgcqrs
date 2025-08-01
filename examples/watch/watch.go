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

const app = "example.watch"
const stream = "watch"

type Event struct {
	Word   *string `json:"word,omitempty"`
	Number *int    `json:"number,omitempty"`
}

type matchEventWord struct {
	Word string `json:"word"`
}

// main provides an example of matching a subset of documents via the query2 interface.
func main() {
	//
	// Given a test environment
	//
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()

	//
	// And a unique stream name for this test
	//
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	//
	//And a connection to the target system
	//
	cfg := v1.NewConfig().LoadEnv()
	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}

	stream := sys.MustStream(ctx, app, streamName)

	//
	// And there are existing events
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
	stream.MustSubmit(ctx, kind3, &Event{Word: targetWordPtr})

	//
	// When we establish a watch
	//
	subBuilder := query2.NewQuery(stream)
	sync := make(chan Event, 5)
	defer close(sync)
	subBuilder.OnKind(kind2).Subset(&Event{Word: targetWordPtr}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		sync <- entity
	}))

	if err := subBuilder.Watch(ctx); err != nil {
		panic(err)
	}

	//
	// Then we should receive the original event
	//
	firstEvent, err := receiveOrTimeout[Event](ctx, sync, 500*time.Millisecond)
	if err != nil {
		panic(err)
	}
	if *firstEvent.Number != value1 {
		fmt.Printf("FAILED -- expected first event with number %d, got %d\n", value1, *firstEvent.Number)
		os.Exit(-1)
	}

	//
	// When another event is recorded
	//
	stream.MustSubmit(ctx, kind2, &Event{Word: targetWordPtr, Number: &value2})

	//
	// Then the event is propagated
	//
	secondEvent, err := receiveOrTimeout[Event](ctx, sync, 500*time.Millisecond)
	if err != nil {
		panic(err)
	}
	if *secondEvent.Number != value2 {
		fmt.Printf("FAILED -- expected first event with number %d, got %d\n", value2, *secondEvent.Number)
		os.Exit(-1)
	}

	fmt.Printf("Success\n")
}

func receiveOrTimeout[T any](ctx context.Context, from <-chan T, maximumWait time.Duration) (out T, problem error) {
	select {
	case <-ctx.Done():
		return out, ctx.Err()
	case v := <-from:
		return v, nil
	case <-time.After(maximumWait):
		return out, fmt.Errorf("timed out waiting for message")
	}
}
