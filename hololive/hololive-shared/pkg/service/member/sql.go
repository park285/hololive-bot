package member

import (
	"embed"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	addAliasSQL                   = mustSQL("add_alias.sql")
	countMembersSQL               = mustSQL("count_members.sql")
	createMemberSQL               = mustSQL("create_member.sql")
	findByAliasSQL                = mustSQL("find_by_alias.sql")
	findByChannelIDSQL            = mustSQL("find_by_channel_id.sql")
	findByNameSQL                 = mustSQL("find_by_name.sql")
	getAllChannelIDsSQL           = mustSQL("get_all_channel_ids.sql")
	getAllMembersSQL              = mustSQL("get_all_members.sql")
	getMembersNeedingPhotoSyncSQL = mustSQL("get_members_needing_photo_sync.sql")
	getMembersWithPhotoSQL        = mustSQL("get_members_with_photo.sql")
	matchCreatedMemberSQL         = mustSQL("match_created_member.sql")
	removeAliasSQL                = mustSQL("remove_alias.sql")
	setGraduationSQL              = mustSQL("set_graduation.sql")
	updateChannelIDSQL            = mustSQL("update_channel_id.sql")
	updateMemberNameSQL           = mustSQL("update_member_name.sql")
	updatePhotoSQL                = mustSQL("update_photo.sql")
)

// 기념일 조회 SQL은 상수 열 목록만 끼워 넣으므로 호출마다 Sprintf하지 않고 초기화 때 완성한다.
var (
	findMembersWithAnniversaryOnSQL       = fmt.Sprintf(mustSQL("find_members_with_anniversary_on.sql"), celebrationMemberColumns)
	findMembersWithBirthdayOnSQL          = fmt.Sprintf(mustSQL("find_members_with_birthday_on.sql"), celebrationMemberColumns)
	findMembersWithCelebrationsInMonthSQL = fmt.Sprintf(mustSQL("find_members_with_celebrations_in_month.sql"), celebrationMemberColumns, celebrationMemberColumns)
)
