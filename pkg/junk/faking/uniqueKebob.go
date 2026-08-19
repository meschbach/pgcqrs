package faking

import (
	"strings"

	"github.com/go-faker/faker/v4"
)

// NewUniqueKebab creates a UniqueDomain that generates unique kebab-case strings.
func NewUniqueKebab() *UniqueDomain[string] {
	return NewUniqueDomain(func() string {
		count := RandIntRange(1, 5)
		buffer := make([]string, count)
		for i := range count {
			buffer[i] = faker.Word()
		}
		return strings.Join(buffer, "-")
	})
}
