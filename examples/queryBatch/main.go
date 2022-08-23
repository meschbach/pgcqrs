package main

import (
	"context"
	"fmt"
	"github.com/bxcodec/faker/v3"
	"github.com/meschbach/go-junk-bucket/pkg"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"strconv"
	"time"
)

const app = "example.query-batch"
const stream = "batch-stream"

type Event struct {
	Word string `json:"word"`
}

func main() {
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	url := pkg.EnvOrDefault("PGCQRS_URL", "http://localhost:9000")

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	sys := v1.NewSystem(v1.NewHttpTransport(url))
	stream := sys.MustStream(ctx, app, streamName)

	kind1 := faker.Word()
	target := faker.Word()
	kind2 := faker.Word()
	kind3 := faker.Word()
	stream.MustSubmit(ctx, kind1, &Event{Word: faker.Word()})
	example := stream.MustSubmit(ctx, kind2, &Event{Word: target})
	stream.MustSubmit(ctx, kind2, &Event{Word: faker.Word()})
	stream.MustSubmit(ctx, kind3, &Event{Word: faker.Word()})
	stream.MustSubmit(ctx, kind3, &Event{Word: target})

	var outEvent Event
	var outEnvelope v1.Envelope
	query := stream.Query()
	query.WithKind(kind2).Match(Event{Word: target}).On(v1.EntityFunc(func(ctx context.Context, e v1.Envelope, entity Event) {
		outEnvelope = e
		outEvent = entity
	}))
	if err := query.Stream(ctx); err != nil {
		panic(err)
	}

	fmt.Printf("%v (%v)\n", example.ID, target)
	fmt.Printf("%v (%v)\n", outEnvelope, outEvent)
}
