package notification

import (
	"errors"
	"reflect"
	"testing"
)

func TestFilterValidClaimKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "only valid claim keys",
			in: []string{
				"notified:claim:room:stream:123:LIVE",
				"notified:claim:event:room:channel:123:event:LIVE",
			},
			want: []string{
				"notified:claim:room:stream:123:LIVE",
				"notified:claim:event:room:channel:123:event:LIVE",
			},
		},
		{
			name: "mixed values",
			in: []string{
				"notified:claim:room:stream:123:LIVE",
				"notified:upcoming:event:room:stream:123",
				"  notified:claim:event:room:channel:123:event:LIVE  ",
				"",
				"alarm:registry",
			},
			want: []string{
				"notified:claim:room:stream:123:LIVE",
				"notified:claim:event:room:channel:123:event:LIVE",
			},
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterValidClaimKeys(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("filterValidClaimKeys() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseEnvelope_VersionCheck(t *testing.T) {
	t.Parallel()

	// 최소 유효 notification JSON (필수 필드만 포함)
	makeRaw := func(version uint8) string {
		return `{"notification":{"room_id":"r1","channel":null,"stream":null,"minutes_until":0,"users":[]},"claim_keys":[],"enqueued_at":"2026-01-01T00:00:00Z","version":` +
			string(rune('0'+version)) + `}`
	}

	tests := []struct {
		name        string
		raw         string
		wantErr     bool
		wantVersion bool // errUnsupportedVersion 여부
	}{
		{
			name:        "v1 정상 처리",
			raw:         makeRaw(1),
			wantErr:     false,
			wantVersion: false,
		},
		{
			name:        "v0 레거시 정상 처리",
			raw:         `{"notification":{"room_id":"r1","channel":null,"stream":null,"minutes_until":0,"users":[]},"claim_keys":[],"enqueued_at":"2026-01-01T00:00:00Z"}`,
			wantErr:     false,
			wantVersion: false,
		},
		{
			name:        "v99 미지원 버전 skip",
			raw:         `{"notification":{"room_id":"r1","channel":null,"stream":null,"minutes_until":0,"users":[]},"claim_keys":[],"enqueued_at":"2026-01-01T00:00:00Z","version":99}`,
			wantErr:     true,
			wantVersion: true,
		},
		{
			name:    "JSON 파싱 오류",
			raw:     `{invalid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, err := parseEnvelope(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEnvelope() error = nil, want non-nil")
				}
				if tt.wantVersion && !errors.Is(err, errUnsupportedVersion) {
					t.Fatalf("parseEnvelope() error = %v, want errUnsupportedVersion", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEnvelope() unexpected error: %v", err)
			}
			if env == nil {
				t.Fatalf("parseEnvelope() = nil, want non-nil")
			}
		})
	}
}
