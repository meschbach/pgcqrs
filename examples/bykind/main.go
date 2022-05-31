package main

import (
	"context"
	"fmt"
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

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	sys := v1.NewSystem(v1.NewHttpTransport(url))
	stream := sys.MustStream(ctx, app, streamName)
	stream.MustSubmit(ctx, "general", &Event{First: true})
	stream.MustSubmit(ctx, "specific", &Event{First: true})
	stream.MustSubmit(ctx, "create", &Event{First: false})
	stream.MustSubmit(ctx, "create", &Event{First: false})
	stream.MustSubmit(ctx, "create", &Event{First: false})
	stream.MustSubmit(ctx, "create", &Event{First: true})
	stream.MustSubmit(ctx, "create", &Event{First: true})
	stream.MustSubmit(ctx, "destroy", &Event{First: true})

	envelopes := stream.MustByKind(ctx, "create")
	fmt.Printf("Create: 5 = %#v ?\n", len(envelopes))
}
