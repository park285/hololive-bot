package migrations

import "embed"

//go:embed manifest.txt *.sql
var FS embed.FS
