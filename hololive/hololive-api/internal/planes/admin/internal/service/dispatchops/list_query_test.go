package dispatchops

import (
	_ "embed"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 실제 projection을 사용하되 DB 연결이나 쿼리 계획 검증을 대신하지 않습니다.
//
//go:embed queries/list.sql
var testListProjection string

func TestListQueryAllFilterCombinations(t *testing.T) {
	for mask := range 16 {
		t.Run(fmt.Sprintf("filters_%04b", mask), func(t *testing.T) {
			filter := Filter{}
			wantPredicates := make([]string, 0, 4)
			wantArgs := make([]any, 0, 5)
			add := func(predicate string, value any) {
				wantArgs = append(wantArgs, value)
				wantPredicates = append(wantPredicates, fmt.Sprintf("%s $%d", predicate, len(wantArgs)))
			}
			if mask&1 != 0 {
				filter.Status = "sent"
				add("d.status =", "sent")
			} else {
				wantPredicates = append(wantPredicates, "d.status IN ('dlq', 'quarantined')")
			}
			if mask&2 != 0 {
				filter.RoomID = "9007199254740993"
				add("d.room_id =", filter.RoomID)
			}
			if mask&4 != 0 {
				filter.ChannelID = "UC-example"
				add("e.channel_id =", filter.ChannelID)
			}
			if mask&8 != 0 {
				filter.BeforeID = "9223372036854775807"
				add("d.id <", int64(9223372036854775807))
			}
			wantArgs = append(wantArgs, PageSize+1)
			wantQuery := testListProjection + "WHERE " + strings.Join(wantPredicates, " AND ") +
				fmt.Sprintf("\nORDER BY d.id DESC\nLIMIT $%d\n", len(wantArgs))
			query, args, err := buildListQuery(testListProjection, filter)
			if err != nil {
				t.Fatal(err)
			}
			if query != wantQuery || !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("query = %q args = %#v; want %q %#v", query, args, wantQuery, wantArgs)
			}
			if strings.Contains(query, " OR ") || strings.Contains(query, "OFFSET") {
				t.Fatalf("optional OR or offset paging returned: %s", query)
			}
		})
	}
}

func TestListQueryKeepsUntrustedValuesInArguments(t *testing.T) {
	filter := Filter{RoomID: "room' OR TRUE--", ChannelID: "channel'; SELECT secret--", BeforeID: "9007199254740993"}
	query, args, err := buildListQuery(testListProjection, filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{filter.RoomID, filter.ChannelID, filter.BeforeID} {
		if strings.Contains(query, value) {
			t.Fatalf("input appeared in SQL: %q", value)
		}
	}
	want := []any{filter.RoomID, filter.ChannelID, int64(9007199254740993), PageSize + 1}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v; want %#v", args, want)
	}
}

func TestListQueryRejectsInvalidInputBeforeProducingSQL(t *testing.T) {
	for _, filter := range []Filter{
		{Status: "all"}, {Status: "dlq' OR TRUE--"}, {RoomID: " room"},
		{ChannelID: "line\nbreak"}, {BeforeID: "01"}, {BeforeID: "0"},
		{BeforeID: "9223372036854775808"}, {RoomID: string([]byte{0xff})},
	} {
		query, args, err := buildListQuery(testListProjection, filter)
		if !errors.Is(err, ErrInvalidInput) || query != "" || args != nil {
			t.Fatalf("invalid filter produced SQL: %q %#v %v", query, args, err)
		}
	}
}

func TestListQueryHasNoStateSharedBetweenCalls(t *testing.T) {
	firstQuery, firstArgs, err := buildListQuery(testListProjection, Filter{Status: "retry", RoomID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	query, args, err := buildListQuery(testListProjection, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "d.room_id =") || !reflect.DeepEqual(args, []any{PageSize + 1}) {
		t.Fatalf("previous filter leaked: %q %#v", query, args)
	}
	args[0] = 0
	if !reflect.DeepEqual(firstArgs, []any{"retry", "first", PageSize + 1}) || !strings.Contains(firstQuery, "d.room_id = $2") {
		t.Fatal("subsequent request mutated earlier query")
	}
}

func FuzzListQueryBindings(f *testing.F) {
	f.Add("dlq", "room", "channel", "9007199254740993")
	f.Add("", "", "", "")
	f.Add("sent", "' OR TRUE--", "한글", "9223372036854775807")
	placeholder := regexp.MustCompile(`\$([0-9]+)`)
	f.Fuzz(func(t *testing.T, status, room, channel, before string) {
		filter := Filter{Status: status, RoomID: room, ChannelID: channel, BeforeID: before}
		query, args, err := buildListQuery(testListProjection, filter)
		if invalid := filter.Validate(); invalid != nil {
			if !errors.Is(err, ErrInvalidInput) || query != "" || args != nil {
				t.Fatal("invalid input produced a query")
			}
			return
		}
		if err != nil || len(args) == 0 || len(args) > 5 || args[len(args)-1] != PageSize+1 {
			t.Fatalf("invalid bindings: %#v %v", args, err)
		}
		matches := placeholder.FindAllStringSubmatch(query, -1)
		if len(matches) != len(args) {
			t.Fatalf("placeholder count differs: %q %#v", query, args)
		}
		for index, match := range matches {
			if match[1] != strconv.Itoa(index+1) {
				t.Fatalf("nonsequential placeholder: %q", match[0])
			}
		}
	})
}
