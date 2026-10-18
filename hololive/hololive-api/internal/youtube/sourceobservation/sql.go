package sourceobservation

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = func() func(string) string {
	read := sqlassets.MustReader(sqlAssets, "queries")
	relation := read("repository_ready_relation.sql")
	claim := strings.ReplaceAll(read("repository_claim_0012_12.sql"), "{{ready_relation}}", strings.ReplaceAll(relation, "{{max_attempts}}", "$6"))
	snapshot := strings.ReplaceAll(read("repository_ready_snapshot.sql"), "{{ready_relation}}", strings.ReplaceAll(relation, "{{max_attempts}}", "$2"))

	return func(name string) string {
		switch name {
		case "repository_claim_0012_12.sql":
			return claim
		case "repository_ready_snapshot.sql":
			return snapshot
		default:
			return read(name)
		}
	}
}()
