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
			input: "indie",
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
