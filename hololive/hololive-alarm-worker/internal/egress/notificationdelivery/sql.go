package notificationdelivery

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	claimReadySQL    = strings.Replace(mustSQL("outbox_claim_ready.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
	readySnapshotSQL = strings.Replace(mustSQL("outbox_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("outbox_ready_predicate.sql"), 1)
)
