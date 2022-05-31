package main

import (
	"context"
	"fmt"
	"github.com/meschbach/go-junk-bucket/pkg"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"strconv"
	"time"
)

const app = "example.largeStream"
const stream = "k10-"

type Event struct {
	First bool  `json:"first"`
	ID    int64 `json:"id"`
}

func main() {
	streamName := stream + strconv.FormatInt(time.Now().Unix(), 36)
	fmt.Printf("Using %q for stream to insert 10K events\n", streamName)

	url := pkg.EnvOrDefault("PGCQRS_URL", "http://localhost:9000")

	ctx, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	sys := v1.NewSystem(v1.NewHttpTransport(url))
	stream := sys.MustStream(ctx, app, streamName)

	kinds := []string{
		"melting",
		"at",
		"the",
		"forge",
		"trail",
		"by",
		"fire",
		"bamboo",
		"fireworks",
		"above",
	}

	first := true
	for i := 0; i < 10000/len(kinds); i++ {
		for _, k := range kinds {
			stream.MustSubmit(ctx, k, Event{
				First: first,
				ID:    int64(i),
			})
		}
		first = false
	}
}
