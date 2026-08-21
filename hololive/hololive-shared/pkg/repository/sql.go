package repository

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	templateListSQL                = mustSQL("template_list.sql")
	templateListByKeySQL           = mustSQL("template_list_by_key.sql")
	templateListByChannelSQL       = mustSQL("template_list_by_channel.sql")
	templateListByKeyAndChannelSQL = mustSQL("template_list_by_key_and_channel.sql")
	templateFindDefaultSQL         = mustSQL("template_find_default.sql")
	templateFindOverrideSQL        = mustSQL("template_find_override.sql")
	templateListOverridesSQL       = mustSQL("template_list_overrides.sql")
	templateUpsertDefaultSQL       = mustSQL("template_upsert_default.sql")
	templateUpsertOverrideSQL      = mustSQL("template_upsert_override.sql")
	templateDeleteOverrideSQL      = mustSQL("template_delete_override.sql")
	revisionInsertSQL              = mustSQL("revision_insert.sql")
	revisionInsertAtClockSQL       = mustSQL("revision_insert_at_clock.sql")
	revisionListSQL                = mustSQL("revision_list.sql")
	revisionGetSQL                 = mustSQL("revision_get.sql")
	revisionPruneSQL               = mustSQL("revision_prune.sql")
)
