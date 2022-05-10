package junk

func Must(maybeError error) {
	if maybeError != nil {
		panic(maybeError)
	}
}
