package main

import (
	"context"
	"fmt"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"time"
)

const app = "example-simple"
const stream = "base"

type Event struct {
	First bool `json:"first"`
}

func main() {
	url := "http://localhost:9000"

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	client := v1.NewClient(url)
	junk.Must(client.NewStream(ctx, app, stream))
	reply, err := client.Submit(ctx, app, stream, "general", &Event{First: true})
	junk.Must(err)

	var byID Event
	junk.Must(client.GetEvent(ctx, app, stream, reply.Id, &byID))
	fmt.Printf("Event by ID %#v\n", byID)

	envelopes, err := client.AllEnvelopes(ctx, app, stream)
	junk.Must(err)

	fmt.Printf("Events: %d\n", len(envelopes.Envelopes))
	for _, envelope := range envelopes.Envelopes {
		var byMeta Event
		junk.Must(client.GetEvent(ctx, app, stream, envelope.ID, &byMeta))
		fmt.Printf("\t%#v: %#v\n", envelope, byMeta)
	}
}
