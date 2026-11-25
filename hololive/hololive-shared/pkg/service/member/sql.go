package member

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	addAliasSQL           = mustSQL("add_alias.sql")
	countMembersSQL       = mustSQL("count_members.sql")
	createMemberSQL       = mustSQL("create_member.sql")
	findByAliasSQL        = mustSQL("find_by_alias.sql")
	findByChannelIDSQL    = mustSQL("find_by_channel_id.sql")
	findByNameSQL         = mustSQL("find_by_name.sql")
	getAllChannelIDsSQL   = mustSQL("get_all_channel_ids.sql")
	getAllMembersSQL      = mustSQL("get_all_members.sql")
	matchCreatedMemberSQL = mustSQL("match_created_member.sql")
	removeAliasSQL        = mustSQL("remove_alias.sql")
	setGraduationSQL      = mustSQL("set_graduation.sql")
	updateChannelIDSQL    = mustSQL("update_channel_id.sql")
	updateMemberNameSQL   = mustSQL("update_member_name.sql")
)

var (
	findMembersWithAnniversaryOnSQL       = mustSQL("find_members_with_anniversary_on.sql").WithToken("{{memberColumns}}", celebrationMemberColumns)
	findMembersWithBirthdayOnSQL          = mustSQL("find_members_with_birthday_on.sql").WithToken("{{memberColumns}}", celebrationMemberColumns)
	findMembersWithCelebrationsInMonthSQL = mustSQL("find_members_with_celebrations_in_month.sql").WithToken("{{memberColumns}}", celebrationMemberColumns)
)
