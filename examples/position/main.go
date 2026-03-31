package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

type UserEvent struct {
	UserID   string `json:"userID"`
	Action   string `json:"action"`
	Metadata string `json:"metadata"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Initialize System with Memory Transport
	// In a real application, you would use GRPCTransport or HTTPTransport
	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)

	domain := "example-app"
	streamName := "user-activity"
	stream := system.MustStream(ctx, domain, streamName)

	// 2. Submit initial events
	fmt.Println("Submitting initial events...")
	for i := 0; i < 5; i++ {
		stream.MustSubmit(ctx, "UserActivity", &UserEvent{
			UserID:   faker.UUIDHyphenated(),
			Action:   "login",
			Metadata: faker.Sentence(),
		})
	}

	consumerName := "activity-indexer-v1"

	// 3. Process events from the beginning
	fmt.Printf("\nProcessing events from the beginning for consumer %q\n", consumerName)
	lastProcessedID := processEvents(ctx, stream, -1)
	fmt.Printf("Last processed ID: %d\n", lastProcessedID)

	// 4. Save consumer position
	_, err := system.Transport.SetPosition(ctx, domain, streamName, consumerName, lastProcessedID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Saved position: %d for %q\n", lastProcessedID, consumerName)

	// 5. Submit more events
	fmt.Println("\nSubmitting more events...")
	for i := 0; i < 3; i++ {
		stream.MustSubmit(ctx, "UserActivity", &UserEvent{
			UserID:   faker.UUIDHyphenated(),
			Action:   "click",
			Metadata: faker.Sentence(),
		})
	}

	// 6. Resume from saved position
	savedPosition, found, err := system.Transport.GetPosition(ctx, domain, streamName, consumerName)
	if err != nil {
		panic(err)
	}
	if found {
		fmt.Printf("\nResuming from saved position %d for consumer %q\n", savedPosition, consumerName)
		newLastID := processEvents(ctx, stream, savedPosition)
		fmt.Printf("New last processed ID: %d\n", newLastID)

		// Update position
		_, err = system.Transport.SetPosition(ctx, domain, streamName, consumerName, newLastID)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Updated position: %d for %q\n", newLastID, consumerName)
	}
}

func processEvents(ctx context.Context, stream *v1.Stream, afterID int64) int64 {
	var lastID int64 = afterID
	count := 0

	q := query2.NewQuery(stream)
	if afterID >= 0 {
		q.After(afterID)
	}

	q.OnKind("UserActivity").Each(func(ctx context.Context, env v1.Envelope, data json.RawMessage) error {
		var event UserEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		fmt.Printf("  [Event %d] User: %s, Action: %s\n", env.ID, event.UserID, event.Action)
		lastID = env.ID
		count++
		return nil
	})

	err := q.StreamBatch(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Processed %d events\n", count)
	return lastID
}
