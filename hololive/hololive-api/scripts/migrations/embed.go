package migrations

import "embed"

//go:embed manifest.txt *.sql contracts/*.sql
var FS embed.FS
