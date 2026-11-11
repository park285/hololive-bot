package workerapp

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var youtubeDeliveryExpiredSnapshotSQL = mustSQL("youtube_delivery_expired_snapshot.sql")
