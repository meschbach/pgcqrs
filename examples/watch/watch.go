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
	sync := make(chan Event, 25)
	defer close(sync)
	subBuilder.OnKind(kind2).Subset(&Event{Word: targetWordPtr}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		fmt.Printf("CCC Received event %d\n", e.ID)
		select {
		case sync <- entity:
			fmt.Printf("CCC\t\tevent %d pushed\n", e.ID)
		case <-ctx.Done():
			panic(ctx.Err())
		}
	}))

	pump, err := subBuilder.Watch(ctx)
	if err != nil {
		panic(err)
	}

	//
	// Then we should receive the original event
	//
	firstEvent, err := tickPump[Event](ctx, pump, 500*time.Millisecond, sync)
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
	secondEvent, err := tickPump[Event](ctx, pump, 500*time.Millisecond, sync)
	if err != nil {
		panic(err)
	}
	if *secondEvent.Number != value2 {
		fmt.Printf("FAILED -- expected first event with number %d, got %d\n", value2, *secondEvent.Number)
		os.Exit(-1)
	}

	fmt.Printf("Success\n")
}

func tickPump[T any](parent context.Context, pump *query2.Watch, maximumWait time.Duration, from <-chan T) (out T, problem error) {
	timedContext, done := context.WithTimeout(parent, maximumWait)
	defer done()

	if err := pump.Tick(timedContext); err != nil {
		return out, err
	}

	select {
	case <-timedContext.Done():
		return out, timedContext.Err()
	case v, ok := <-from:
		if !ok {
			return out, fmt.Errorf("sync channel not okay")
		}
		return v, nil
	}
}
