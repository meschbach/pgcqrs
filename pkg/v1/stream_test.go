package v1

import (
	"context"
	"github.com/bxcodec/faker/v3"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type PutEvent struct {
	Value string `json:"value"`
}
type putEventQuery = PutEvent

func TestFindByKindsWithMultiple(t *testing.T) {
	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	mem := NewMemoryTransport()
	system := NewSystem(mem)
	stream := system.MustStream(ctx, "test", "TestFindByKindsWithMultiple")

	kind1 := faker.Name()
	stream.MustSubmit(ctx, kind1, &PutEvent{Value: kind1})
	stream.MustSubmit(ctx, kind1, &PutEvent{Value: kind1})

	otherKind := faker.LastName()
	stream.MustSubmit(ctx, otherKind, &PutEvent{Value: otherKind})
	stream.MustSubmit(ctx, otherKind, &PutEvent{Value: otherKind})

	kind2 := faker.Email()
	stream.MustSubmit(ctx, kind2, &PutEvent{Value: kind2})

	envelopes := stream.MustByKind(ctx, kind1, kind2)
	if assert.Len(t, envelopes, 3) {
		var entity PutEvent
		stream.MustGet(ctx, envelopes[0].ID, &entity)
		assert.Equal(t, kind1, entity.Value)
		stream.MustGet(ctx, envelopes[1].ID, &entity)
		assert.Equal(t, kind1, entity.Value)
		stream.MustGet(ctx, envelopes[2].ID, &entity)
		assert.Equal(t, kind2, entity.Value)
	}
}
