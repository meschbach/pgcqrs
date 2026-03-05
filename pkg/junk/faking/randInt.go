// Package faking provides utilities for generating random test data.
package faking

import (
	"math/rand/v2"

	"github.com/go-faker/faker/v4"
)

// RandIntRange returns a random integer in the range [minimum, maximum).
func RandIntRange(minimum, maximum int) int {
	values, err := faker.RandomInt(minimum, maximum, 1)
	if err != nil {
		panic(err)
	}
	return values[0]
}

// RandInt returns a random integer.
func RandInt() int {
	// Linter wants cryptographically secure random.  In a testing context we do not require that.
	// nolint
	oversized := rand.Int64()
	return int(oversized)
}
