package faking

import "github.com/go-faker/faker/v4"

func RandIntRange(min, max int) int {
	values, err := faker.RandomInt(min, max, 1)
	if err != nil {
		panic(err)
	}
	return values[0]
}

func RandInt() int {
	return RandIntRange(-10000, 10000)
}
