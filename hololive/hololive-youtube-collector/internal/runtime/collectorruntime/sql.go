package collectorruntime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	sqlLoadContractGenerations  = mustSQL("load_contract_generations.sql")
	sqlObservationHandoffStatus = mustSQL("observation_handoff_status.sql")
	sqlPendingObservationCount  = mustSQL("pending_observation_count.sql")
)
