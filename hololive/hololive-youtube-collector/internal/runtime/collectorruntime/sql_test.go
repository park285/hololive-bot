package collectorruntime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed testqueries/*
var testSQLAssets embed.FS

var mustTestSQL = sqlassets.MustReader(testSQLAssets, "testqueries")

// 테스트 전용 SQL 자산도 패키지 초기화 때 한 번 읽는다.
var seedObservationQueueSQL = mustTestSQL("seed_observation_queue.sql")
