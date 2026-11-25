package collectorruntime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	loadContractGenerationsSQL  = mustSQL("load_contract_generations.sql")
	observationHandoffStatusSQL = mustSQL("observation_handoff_status.sql")
	pendingObservationCountSQL  = mustSQL("pending_observation_count.sql")
)
