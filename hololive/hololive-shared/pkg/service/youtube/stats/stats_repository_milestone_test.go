// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
