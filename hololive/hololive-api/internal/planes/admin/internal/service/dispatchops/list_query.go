package dispatchops

import (
	"strconv"
	"strings"
)

// errorCodeExpression은 목록 표시와 실패 필터·집계가 공유하는 공개 오류 분류식입니다.
const errorCodeExpression = "(CASE WHEN d.last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$' THEN d.last_error_code WHEN d.last_error_code = '' THEN '' ELSE 'unclassified' END)"

// buildListQuery는 고정 projection과 실제 필터만 조합합니다. 사용자 값은 SQL에 보간하지 않습니다.
// 필터 조합은 64가지로 제한되며, 선택하지 않은 조건의 OR 분기를 DB 계획에 남기지 않습니다.
func buildListQuery(projection string, filter Filter) (string, []any, error) {
	if err := filter.Validate(); err != nil {
		return "", nil, err
	}

	predicates := make([]string, 0, 6)
	args := make([]any, 0, 7)
	bind := func(predicate string, value any) {
		args = append(args, value)
		predicates = append(predicates, predicate+" $"+strconv.Itoa(len(args)))
	}

	if filter.Status == "" {
		predicates = append(predicates, "d.status IN ('dlq', 'quarantined')")
	} else {
		bind("d.status =", filter.Status)
	}

	if filter.RoomID != "" {
		bind("d.room_id =", filter.RoomID)
	}

	if filter.ChannelID != "" {
		bind("e.channel_id =", filter.ChannelID)
	}

	if filter.BeforeID != "" {
		before, err := ParseID(filter.BeforeID)
		if err != nil {
			return "", nil, err
		}

		bind("d.id <", before)
	}

	if filter.AlarmType != "" {
		bind("e.alarm_type::text =", filter.AlarmType)
	}

	if filter.ErrorCode != "" {
		bind(errorCodeExpression+" =", filter.ErrorCode)
	}

	args = append(args, PageSize+1)

	query := projection + "WHERE " + strings.Join(predicates, " AND ") +
		"\nORDER BY d.id DESC\nLIMIT $" + strconv.Itoa(len(args)) + "\n"

	return query, args, nil
}
