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

package holodex

import (
	stdErrors "errors"
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestResolveStreamOrg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "empty defaults to hololive",
			input: "",
			want:  constants.HolodexAPIParams.OrgHololive,
		},
		{
			name:  "hololive alias",
			input: "hololive",
			want:  constants.HolodexAPIParams.OrgHololive,
		},
		{
			name:  "vspo org",
			input: "VSPO",
			want:  constants.HolodexAPIParams.OrgVSpo,
		},
		{
			name:  "stellive org",
			input: "stellive",
			want:  constants.HolodexAPIParams.OrgStellive,
		},
		{
			name:  "indie org",
			input: "independents",
			want:  constants.HolodexAPIParams.OrgIndie,
		},
		{
			name:  "all org",
			input: "all",
			want:  constants.HolodexAPIParams.OrgAll,
		},
		{
			name:    "invalid org",
			input:   "nijisanji",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveStreamOrg(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveStreamOrg(%q) expected error, got nil", tt.input)
				}
				if !stdErrors.Is(err, ErrInvalidStreamOrg) {
					t.Fatalf("resolveStreamOrg(%q) expected ErrInvalidStreamOrg, got %v", tt.input, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("resolveStreamOrg(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("resolveStreamOrg(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStreamTargetOrgs(t *testing.T) {
	t.Parallel()

	got := streamTargetOrgs(constants.HolodexAPIParams.OrgAll)
	want := append([]string{}, constants.HolodexAPIParams.SyncTargetOrgs...)
	want = append(want, constants.HolodexAPIParams.OrgIndie)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("streamTargetOrgs(all) = %v, want %v", got, want)
	}
}

func TestHolodexOrgFetchParallelism(t *testing.T) {
	original := constants.HolodexConcurrencyConfig.OrgAllParallelism
	defer func() {
		constants.HolodexConcurrencyConfig.OrgAllParallelism = original
	}()

	constants.HolodexConcurrencyConfig.OrgAllParallelism = 3
	if got := holodexOrgFetchParallelism(constants.HolodexAPIParams.OrgAll); got != 3 {
		t.Fatalf("holodexOrgFetchParallelism(all) = %d, want 3", got)
	}
	if got := holodexOrgFetchParallelism(constants.HolodexAPIParams.OrgHololive); got != 1 {
		t.Fatalf("holodexOrgFetchParallelism(hololive) = %d, want 1", got)
	}

	constants.HolodexConcurrencyConfig.OrgAllParallelism = 0
	if got := holodexOrgFetchParallelism(constants.HolodexAPIParams.OrgAll); got != 1 {
		t.Fatalf("holodexOrgFetchParallelism(all) with non-positive config = %d, want 1", got)
	}
}

func TestFilterStreamsByRequestedOrg(t *testing.T) {
	t.Parallel()

	hololive := constants.HolodexAPIParams.OrgHololive
	vspo := constants.HolodexAPIParams.OrgVSpo

	streams := []*domain.Stream{
		{
			ID:      "holo-1",
			Channel: &domain.Channel{Org: &hololive},
		},
		{
			ID:      "vspo-1",
			Channel: &domain.Channel{Org: &vspo},
		},
	}

	filtered := filterStreamsByRequestedOrg(streams, constants.HolodexAPIParams.OrgHololive)
	if len(filtered) != 1 || filtered[0].ID != "holo-1" {
		t.Fatalf("filterStreamsByRequestedOrg() = %v, want hololive only", filtered)
	}
}
