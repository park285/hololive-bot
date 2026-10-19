package store

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	claimPendingSQL  = strings.Replace(mustSQL("transition_claim_pending.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
	readySnapshotSQL = strings.Replace(mustSQL("delivery_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
)
