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

const app = "example.bykind"
const stream = "kindstream"

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

	fmt.Printf("%v\n", result.Envelopes())

	result, err = query.Perform(ctx)
	if err != nil {
		panic(err)
	}
}
