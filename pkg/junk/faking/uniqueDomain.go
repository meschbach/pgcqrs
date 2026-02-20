package faking

import "github.com/go-faker/faker/v4"

// UniqueDomain will ensure each invocation of `Next()` returns a unique value from the given `gen` function.
// It is useful when you need to generate unique values for testing or data generation purposes.
// For example, if you're generating test data and want to ensure that each generated value is unique,
// UniqueDomain can help by keeping track of all previously generated values and ensuring that
// the next value is not a duplicate.
//
// Example usage:
//
//  1. Create a UniqueDomain for strings:
//     domain := NewUniqueWords()
//     fmt.Println(domain.Next()) // Outputs a unique word
//     fmt.Println(domain.Next()) // Outputs another unique word
//
//  2. Create a UniqueDomain for integers:
//     domain := NewUniqueInts()
//     fmt.Println(domain.Next()) // Outputs a unique integer
//     fmt.Println(domain.Next()) // Outputs another unique integer
type UniqueDomain[T comparable] struct {
	grouping map[T]bool
	gen      func() T
}

func NewUniqueDomain[T comparable](gen func() T) *UniqueDomain[T] {
	return &UniqueDomain[T]{
		grouping: make(map[T]bool),
		gen:      gen,
	}
}

func (u *UniqueDomain[T]) Next() T {
	retry := 0
	for {
		value := u.gen()
		if _, has := u.grouping[value]; !has {
			u.grouping[value] = true
			return value
		}
		retry++
		if retry >= 8 {
			panic("too many retries")
		}
	}
}

func (u *UniqueDomain[T]) NextPtr() *T {
	value := u.Next()
	return &value
}

func NewUniqueWords() *UniqueDomain[string] {
	return NewUniqueDomain(func() string {
		return faker.Word()
	})
}

func NewUniqueInts() *UniqueDomain[int] {
	return NewUniqueDomain(RandInt)
}
