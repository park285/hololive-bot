package dispatchops

import (
	"strconv"
	"strings"
)

// buildListQuery는 고정 projection과 실제 필터만 조합합니다. 사용자 값은 SQL에 보간하지 않습니다.
// 필터 조합은 16가지로 제한되며, 선택하지 않은 조건의 OR 분기를 DB 계획에 남기지 않습니다.
func buildListQuery(projection string, filter Filter) (string, []any, error) {
	if err := filter.Validate(); err != nil {
		return "", nil, err
	}
	predicates := make([]string, 0, 4)
	args := make([]any, 0, 5)
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
	args = append(args, PageSize+1)
	query := projection + "WHERE " + strings.Join(predicates, " AND ") +
		"\nORDER BY d.id DESC\nLIMIT $" + strconv.Itoa(len(args)) + "\n"
	return query, args, nil
}
