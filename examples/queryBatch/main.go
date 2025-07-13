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

const app = "example.query-batch"
const stream = "batch-stream"

type uniqueDomain[T comparable] struct {
	grouping map[T]bool
	gen      func() T
}

func newUniqueDomain[T comparable](gen func() T) *uniqueDomain[T] {
	return &uniqueDomain[T]{
		grouping: make(map[T]bool),
		gen:      gen,
	}
}

func (u *uniqueDomain[T]) Next() T {
	retry := 0
	for {
		value := u.gen()
		if _, has := u.grouping[value]; !has {
			u.grouping[value] = true
			return value
		}
		retry++
		if retry >= 8 {
			panic("too many retries")
		}
	}
}

func uniqueRandom[T comparable](gen func() T, count int) []T {
	output := make([]T, 0, count)
	grouping := make(map[T]bool)
	retry := 0
	for len(output) < count {
		value := gen()
		if _, has := grouping[value]; !has {
			grouping[value] = true
			output = append(output, value)
			retry = 0
		} else {
			retry++
			if retry > 8 {
				panic("too many retries")
			}
		}
	}
	return output
}

func newUniqueWords() *uniqueDomain[string] {
	return newUniqueDomain(func() string {
		return faker.Word()
	})
}

type Event struct {
	Word string `json:"word"`
}

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

	kinds := newUniqueWords()
	words := newUniqueWords()

	kind1 := kinds.Next()
	target := words.Next()
	kind2 := kinds.Next()
	kind3 := kinds.Next()
	stream.MustSubmit(ctx, kind1, &Event{Word: words.Next()})
	example := stream.MustSubmit(ctx, kind2, &Event{Word: target})
	stream.MustSubmit(ctx, kind2, &Event{Word: words.Next()})
	stream.MustSubmit(ctx, kind3, &Event{Word: words.Next()})
	stream.MustSubmit(ctx, kind3, &Event{Word: target})

	var outEvents []Event
	var outEnvelopes []v1.Envelope
	query := stream.Query()
	query.WithKind(kind2).Match(Event{Word: target}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		outEnvelopes = append(outEnvelopes, e)
		outEvents = append(outEvents, entity)
	}))
	if err := query.Stream(ctx); err != nil {
		panic(err)
	}

	if len(outEnvelopes) != 1 {
		fmt.Printf("FAILED -- expected a single envelope got %d\n%#v\n", len(outEnvelopes), outEnvelopes)
		os.Exit(-1)
	}
	if len(outEvents) != 1 {
		fmt.Printf("FAILED -- envelopes and events not match -- %d\n#%v\n", len(outEvents), outEvents)
		os.Exit(-1)
	}

	fmt.Printf("%v (%v)\n", example.ID, target)
	outEnvelope := outEnvelopes[0]
	outEvent := outEvents[0]
	fmt.Printf("%v (%v)\n", outEnvelope, outEvent)
}
