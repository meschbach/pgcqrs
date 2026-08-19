package views

import "time"

// GetOption configures version-constrained Get operations.
type GetOption interface {
	applyGet(*getOptions)
}

type getOptions struct {
	afterVersion *int64
	untilVersion *int64
	untilTimeout time.Duration
}

// After returns a GetOption that requires the entity version to be at least the given value.
// Returns StatusStale if the entity version is behind.
func After(version int64) GetOption {
	return afterOption{version: version}
}

type afterOption struct {
	version int64
}

func (o afterOption) applyGet(opts *getOptions) {
	opts.afterVersion = &o.version
}

// UntilVersion returns a GetOption that waits up to timeout for the projection to reach
// the given version. Returns StatusTimeout if the version is not reached in time.
func UntilVersion(version int64, timeout time.Duration) GetOption {
	return untilVersionOption{version: version, timeout: timeout}
}

type untilVersionOption struct {
	version int64
	timeout time.Duration
}

func (o untilVersionOption) applyGet(opts *getOptions) {
	opts.untilVersion = &o.version
	opts.untilTimeout = o.timeout
}
