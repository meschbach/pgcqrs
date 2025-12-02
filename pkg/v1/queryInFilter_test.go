package v1

import (
	"encoding/json"
	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type exampleEvent struct {
	ValueString *string `json:"value-string,omitempty"`
	OtherValue  *string `json:"other-value,omitempty"`
}

func TestQueryInFilter(t *testing.T) {
	t.Run("subset filtering", func(t *testing.T) {
		//
		// Given a context
		//
		ctx := t.Context()

		//
		// And a memory interpreter
		//
		m := &memory{
			input:   make(chan memoryOp, 32),
			domains: make(map[string]*memoryDomain),
		}
		go m.runService()

		//
		//
		//
		testStream := faker.Name()
		testDomain := faker.Name()
		require.NoError(t, m.EnsureStream(ctx, testDomain, testStream))

		matchString := faker.Word()
		otherString := faker.Word()
		kind := faker.Name()

		matchPredicate := exampleEvent{
			ValueString: &matchString,
		}
		matchPredicateBytes, err := json.Marshal(matchPredicate)
		require.NoError(t, err)

		matchingDocument := exampleEvent{
			ValueString: &matchString,
			OtherValue:  &otherString,
		}

		submitted, err := m.Submit(ctx, testDomain, testStream, kind, matchingDocument)
		require.NoError(t, err)
		require.NotNil(t, submitted)

		matchOp := int64(faking.RandIntRange(0, 15124332))
		q := &queryInFilter{
			core: m,
			query: &ipc.QueryIn{
				Events: &ipc.DomainStream{
					Domain: testDomain,
					Stream: testStream,
				},
				OnKind: []*ipc.OnKindClause{
					{
						Kind: kind,
						Subsets: []*ipc.OnKindSubsetMatch{
							{
								Match: matchPredicateBytes,
								Op:    matchOp,
							},
						},
					},
				},
			},
		}

		type event struct {
			op       int64
			envelope Envelope
			data     json.RawMessage
		}
		var seen *event
		eventID := submitted.ID
		err = q.filter(ctx, Envelope{
			ID:   eventID,
			When: time.Now().Format(time.RFC3339Nano),
			Kind: kind,
		}, func(op int64, envelope Envelope, message json.RawMessage) {
			seen = &event{
				op:       op,
				envelope: envelope,
				data:     message,
			}
		})
		require.NoError(t, err)

		assert.NotNil(t, seen)
	})
}
