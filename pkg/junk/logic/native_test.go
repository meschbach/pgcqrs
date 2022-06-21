package logic

import (
	"context"
	"github.com/bxcodec/faker/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type StringStruct struct {
	StringField string
}

func TestNativeLookup(t *testing.T) {
	t.Run("get int value", func(t *testing.T) {
		ctx, done := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer done()

		example := 139

		le := NewLogicEngine()
		type IntStruct struct {
			Value int
		}
		result, err := le.Query(ctx, "struct_prop(?,'Value',FieldValue).", &StructTerm{
			Tag:  "IntStruct",
			What: IntStruct{Value: example},
		})
		require.NoError(t, err)
		require.True(t, result.Next(), "must have solution")

		var out struct {
			FieldValue int
		}
		require.NoError(t, result.Scan(&out))
		assert.Equal(t, example, out.FieldValue)
		assert.False(t, result.Next(), "Single solution only")
	})

	t.Run("get string value", func(t *testing.T) {
		ctx, done := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer done()

		example := faker.Sentence()
		le := NewLogicEngine()
		result, err := le.Query(ctx, "struct_prop(?,'StringField',FieldValue).", &StructTerm{
			Tag:  "StringStruct",
			What: StringStruct{StringField: example},
		})
		require.NoError(t, err)
		require.True(t, result.Next(), "must have solution")
		defer result.Close()

		var out struct {
			FieldValue string
		}
		require.NoError(t, result.Scan(&out))
		assert.Equal(t, example, out.FieldValue)
		assert.False(t, result.Next())
	})
}
