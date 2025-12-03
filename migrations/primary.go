package migrations

import "embed"

//go:embed "primary/*.sql"
var Primary embed.FS
