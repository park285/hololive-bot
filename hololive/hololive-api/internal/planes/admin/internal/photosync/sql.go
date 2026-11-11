package photosync

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	membersNeedingSyncSQL = mustSQL("get_members_needing_photo_sync.sql")
	updatePhotoSQL        = mustSQL("update_photo.sql")
)
