// Package junk provides miscellaneous utilities for testing and development.
package junk

import "errors"

// TODO is a reusable error indicating  feature is missing or incomplete.
// nolint
var TODO = errors.New("TODO")
