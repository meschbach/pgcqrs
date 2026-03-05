// Package migrations provides embedded SQL migrations.
package migrations

import "embed"

// Primary is an embedded file system containing the primary migrations.
//
//go:embed "primary/*.sql"
var Primary embed.FS
