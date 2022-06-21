package plql

import (
	"context"
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type TokenEvent struct {
	Token string `json:"token"`
}

type ExampleOut struct {
	Out string
}

const ExampleQuery = `
envelope(_, _, Kind) :- Kind = 'Test'.
event(_, Data) :- T = 'Decently Long Token', gjson_path(Data,'token', T).
transform(Data,O) :- gjson_path(Data,'token',O).  
`

func TestPLQL(t *testing.T) {
	t.Run("Simple extraction example", func(t *testing.T) {
		ctx, done := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer done()

		exampleToken := "Decently Long Token"
		sys := v1.NewSystem(v1.NewMemoryTransport())
		stream := sys.MustStream(ctx, "plql", "test")
		reply := stream.MustSubmit(ctx, "Test", TokenEvent{Token: exampleToken})
		stream.MustSubmit(ctx, "Test", TokenEvent{Token: faker.Word()})
		stream.MustSubmit(ctx, "Nope", TokenEvent{Token: faker.Word()})

		result := PLQL[ExampleOut](ctx, stream, ExampleQuery)
		if assert.Len(t, result.Matching, 1) {
			assert.Equal(t, exampleToken, result.Matching[0].Data.Out)
			assert.Equal(t, reply.ID, result.Matching[0].Envelope.ID)
		}
	})

	t.Run("Update example", func(t *testing.T) {
		type Create struct {
			InitValue string
		}
		createKind := faker.Word()
		type Update struct {
			UpdateValue string
		}
		updateKind := faker.Word()

		ctx, done := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer done()

		domainName := faker.Word()
		streamName := faker.Word()

		sys := v1.NewSystem(v1.NewMemoryTransport())
		stream := sys.MustStream(ctx, domainName, streamName)
		createExample := Create{InitValue: faker.Sentence()}
		createReply := stream.MustSubmit(ctx, createKind, createExample)

		updateExample := Update{UpdateValue: faker.Sentence()}
		updateReply := stream.MustSubmit(ctx, updateKind, updateExample)

		pl := `
kind_type(Env, create, CreateKind) :- struct_prop(Env, 'CreateKind', CreateKind).
kind_type(Env, update, UpdateKind) :- struct_prop(Env, 'UpdateKind', UpdateKind).

envelope_id(Envelope, ID) :- struct_prop(Envelope, 'ID', ID).
envelope_kind(Envelope, Kind) :- struct_prop(Envelope, 'Kind', Kind).


kind(Env, Envelope, Type) :-
	envelope_kind(Envelope, Kind),
	kind_type(Env,Type,Kind).

envelope(Env, Envelope, StateChanges) :-
	kind(Env,Envelope, Type),
	(Type = create; Type = update).

event(Env, Envelope, Data, IState, OState) :-
	envelope_kind(Envelope, Kind),
	kind(State,Type,Kind),
	(Type = create, envelope_id(Envelope, ID)
	; Type = update).
event(envelope(_,_,'%s'), Data) :- gjson_path(Data,'entity', T).
transform(I,I).
`
	})
}
