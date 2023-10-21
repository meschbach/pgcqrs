package main

import (
	"context"
	"fmt"
	"github.com/go-faker/faker/v4"
	"github.com/meschbach/go-junk-bucket/pkg"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"strconv"
	"time"
)

const app = "example.bykind"
const stream = "kindstream"

type Event struct {
	First bool `json:"first"`
}

func main() {
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream\n", streamName)

	url := pkg.EnvOrDefault("PGCQRS_URL", "http://localhost:9000")
	kind1 := faker.Name()
	kind2 := faker.Name()
	kind3 := faker.Name()
	kind4 := faker.Name()

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	sys := v1.NewSystem(v1.NewHttpTransport(url))
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
}
