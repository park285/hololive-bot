package stats

import (
	"reflect"
	"testing"
)

func TestBuildMilestoneWhereClause(t *testing.T) {
	testCases := []struct {
		name          string
		filter        MilestoneFilter
		wantWhere     string
		wantArgs      []any
		wantNextArgID int
	}{
		{
			name:          "empty filter",
			filter:        MilestoneFilter{},
			wantWhere:     "",
			wantArgs:      []any{},
			wantNextArgID: 1,
		},
		{
			name: "channel only",
			filter: MilestoneFilter{
				ChannelID: "UC123",
			},
			wantWhere:     "WHERE channel_id = $1",
			wantArgs:      []any{"UC123"},
			wantNextArgID: 2,
		},
		{
			name: "member only",
			filter: MilestoneFilter{
				MemberName: "miko",
			},
			wantWhere:     "WHERE member_name ILIKE $1",
			wantArgs:      []any{"%miko%"},
			wantNextArgID: 2,
		},
		{
			name: "channel and member",
			filter: MilestoneFilter{
				ChannelID:  "UC123",
				MemberName: "miko",
			},
			wantWhere:     "WHERE channel_id = $1 AND member_name ILIKE $2",
			wantArgs:      []any{"UC123", "%miko%"},
			wantNextArgID: 3,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotWhere, gotArgs, gotNextArgID := buildMilestoneWhereClause(tc.filter)

			if gotWhere != tc.wantWhere {
				t.Fatalf("where = %q, want %q", gotWhere, tc.wantWhere)
			}
			if !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", gotArgs, tc.wantArgs)
			}
			if gotNextArgID != tc.wantNextArgID {
				t.Fatalf("nextArgID = %d, want %d", gotNextArgID, tc.wantNextArgID)
			}
		})
	}
}
