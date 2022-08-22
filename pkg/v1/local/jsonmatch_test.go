package local

import (
	"encoding/json"
	"github.com/bxcodec/faker/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
)

type Superset struct {
	Value    string `json:"value,omitempty"`
	IntValue int    `json:"int-value,omitempty"`
	Blah     string `json:"blah,omitempty"`
}

type Subset struct {
	Value    string `json:"value,omitempty"`
	IntValue int    `json:"int-value,omitempty"`
}

func TestJSONMatch(t *testing.T) {
	t.Run("subset match", func(t *testing.T) {
		value := faker.Word()
		i := rand.Int()
		superset := Superset{
			Value:    value,
			IntValue: i,
			Blah:     faker.Word(),
		}
		supersetBytes, err := json.Marshal(superset)
		require.NoError(t, err)

		subset := Subset{
			Value:    value,
			IntValue: i,
		}
		subsetBytes, err := json.Marshal(subset)
		require.NoError(t, err)

		assert.True(t, JSONIsSubset(supersetBytes, subsetBytes))
	})

	t.Run("exact match", func(t *testing.T) {
		value := faker.Word()
		i := rand.Int()
		superset := Superset{
			Value:    value,
			IntValue: i,
			Blah:     faker.Word(),
		}
		supersetBytes, err := json.Marshal(superset)
		require.NoError(t, err)

		assert.True(t, JSONIsSubset(supersetBytes, supersetBytes))
	})
}
