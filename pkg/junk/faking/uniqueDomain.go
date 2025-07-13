package faking

import "github.com/go-faker/faker/v4"

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

func NewUniqueWords() *UniqueDomain[string] {
	return NewUniqueDomain(func() string {
		return faker.Word()
	})
}

func NewUniqueInts() *UniqueDomain[int] {
	return NewUniqueDomain(func() int {
		return RandInt()
	})
}
