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

const app = "example.query"
const stream = "match-int-"

type Event struct {
	Value int `json:"id"`
}

func randInt() int {
	values, err := faker.RandomInt(-10000, 10000, 1)
	if err != nil {
		panic(err)
	}
	return values[0]
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

	kind1 := faker.Word()
	target := randInt()
	kind2 := faker.Word()
	kind3 := faker.Word()
	stream.MustSubmit(ctx, kind1, &Event{Value: randInt()})
	expectedResult := stream.MustSubmit(ctx, kind2, &Event{Value: target})
	stream.MustSubmit(ctx, kind2, &Event{Value: randInt()})
	stream.MustSubmit(ctx, kind3, &Event{Value: randInt()})
	stream.MustSubmit(ctx, kind3, &Event{Value: target})

	query := stream.Query()
	query.WithKind(kind2).Match(Event{Value: target})
	result, err := query.Perform(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%v\n", result.Envelopes())
	if result.Envelopes()[0].ID == expectedResult.ID {
		fmt.Println("Success")
	} else {
		fmt.Println("Failure")
	}

	result, err = query.Perform(ctx)
	if err != nil {
		panic(err)
	}
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
