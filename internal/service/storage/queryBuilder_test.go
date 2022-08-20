package storage

import (
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestKindConstraint(t *testing.T) {
	t.Run("Just kind", func(t *testing.T) {
		t.Parallel()

		kind := faker.Word()
		out := &SQLQuery{first: true}
		translateKindConstraint(out, v1.KindConstraint{Kind: kind})
		assert.Equal(t, "( kind = $1 )", out.DML)
		if assert.Len(t, out.Args, 1) {
			assert.Equal(t, kind, out.Args[0])
		}
	})
	t.Run("eq property", func(t *testing.T) {
		t.Parallel()

		kind := faker.Word()
		prop := faker.Word()
		value := faker.Word()

		out := &SQLQuery{first: true}
		translateKindConstraint(out, v1.KindConstraint{
			Kind: kind,
			Eq: []v1.WireMatcherV1{
				v1.WireMatcherV1{
					Property: []string{prop},
					Value:    []string{value},
				},
			},
		})
		assert.Equal(t, "( kind = $1 AND event@>$2 IN ( $3 ) )", out.DML)
		if assert.Len(t, out.Args, 3) {
			assert.Equal(t, kind, out.Args[0])
			assert.Equal(t, "{"+prop+"}", out.Args[1])
			assert.Equal(t, value, out.Args[2])
		}
	})
}

func TestQueryTranslator(t *testing.T) {
	t.Parallel()

	t.Run("No kinds", func(t *testing.T) {
		t.Parallel()

		app := faker.Word()
		stream := faker.Word()

		query := TranslateQuery(app, stream, v1.WireQuery{})
		assert.Equal(t, "SELECT id, when_occurred, kind FROM events WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2) ORDER BY when_occurred ASC", query.DML)
		if assert.Len(t, query.Args, 2) {
			assert.Equal(t, app, query.Args[0])
			assert.Equal(t, stream, query.Args[1])
		}
	})
	t.Run("Single kind", func(t *testing.T) {
		t.Parallel()

		app := faker.Word()
		stream := faker.Word()
		kind := faker.Word()

		input := v1.WireQuery{
			KindConstraint: []v1.KindConstraint{
				v1.KindConstraint{Kind: kind},
			},
		}
		query := TranslateQuery(app, stream, input)
		assert.Equal(t, "SELECT id, when_occurred, kind FROM events WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2) AND ( ( kind = $3 ) ) ORDER BY when_occurred ASC", query.DML)
		if assert.Len(t, query.Args, 3) {
			assert.Equal(t, app, query.Args[0])
			assert.Equal(t, stream, query.Args[1])
			assert.Equal(t, kind, query.Args[2])
		}
	})

	t.Run("Two kinds", func(t *testing.T) {
		t.Parallel()

		app := faker.Word()
		stream := faker.Word()
		kind1 := faker.Word()
		kind2 := faker.Word()

		input := v1.WireQuery{
			KindConstraint: []v1.KindConstraint{
				v1.KindConstraint{Kind: kind2},
				v1.KindConstraint{Kind: kind1},
			},
		}
		query := TranslateQuery(app, stream, input)
		assert.Equal(t, "SELECT id, when_occurred, kind FROM events WHERE stream_id = (SELECT id FROM events_stream WHERE app = $1 AND stream = $2) AND ( ( kind = $3 ) OR ( kind = $4 ) ) ORDER BY when_occurred ASC", query.DML)
		if assert.Len(t, query.Args, 4) {
			assert.Equal(t, app, query.Args[0])
			assert.Equal(t, stream, query.Args[1])
			assert.Equal(t, kind2, query.Args[2])
			assert.Equal(t, kind1, query.Args[3])
		}
	})
}
