package info

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestOrgFallbackGroups(t *testing.T) {
	tests := []struct {
		name   string
		member *domain.Member
		want   []string
	}{
		{name: "nil 멤버", member: nil, want: nil},
		{name: "mekPark은 전용 그룹", member: &domain.Member{Org: "mekPark"}, want: []string{"mekPark"}},
		{name: "Hololive는 폴백(기타)", member: &domain.Member{Org: "Hololive"}, want: nil},
		{name: "Stellive는 폴백(기타)", member: &domain.Member{Org: "Stellive"}, want: nil},
		{name: "빈 org는 폴백(기타)", member: &domain.Member{Org: ""}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orgFallbackGroups(tt.member)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("orgFallbackGroups() = %v, want %v", got, tt.want)
			}
		})
	}
}
