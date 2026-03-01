package notification

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// TestParseEnvelope_RustFixtureCompat: Rust가 생성한 fixture를 Go consumer가 정상 파싱하는지 검증.
// 교차언어 큐 계약 테스트 — fixture 변경 시 양측 테스트 모두 통과해야 함.
func TestParseEnvelope_RustFixtureCompat(t *testing.T) {
	t.Parallel()

	// 테스트 CWD는 패키지 디렉토리이므로 repo root까지 4단계 상위로 이동
	fixtureDir := filepath.Join("..", "..", "..", "..", "hololive-rs", "testdata", "alarm_queue")

	t.Run("v1 normal envelopes", func(t *testing.T) {
		t.Parallel()

		// alarm_queue_envelope.json: JSON array 3건
		data, err := os.ReadFile(filepath.Join(fixtureDir, "alarm_queue_envelope.json"))
		if err != nil {
			t.Fatalf("fixture 읽기 실패: %v", err)
		}

		var raws []json.RawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			t.Fatalf("JSON array 파싱 실패: %v", err)
		}
		if len(raws) != 3 {
			t.Fatalf("fixture 건수 = %d, want 3", len(raws))
		}

		for i, raw := range raws {
			env, err := parseEnvelope(string(raw))
			if err != nil {
				t.Errorf("[%d] parseEnvelope() 오류 = %v", i, err)
				continue
			}
			// 버전 검증
			if env.Version != 1 {
				t.Errorf("[%d] Version = %d, want 1", i, env.Version)
			}
			// RoomID 비어있지 않음
			if env.Notification.RoomID == "" {
				t.Errorf("[%d] Notification.RoomID 비어있음", i)
			}
			// claim_keys 모두 NotifyClaimKeyPrefix 보유
			for _, key := range env.ClaimKeys {
				if !strings.HasPrefix(key, NotifyClaimKeyPrefix) {
					t.Errorf("[%d] claim key %q가 prefix %q를 가지지 않음", i, key, NotifyClaimKeyPrefix)
				}
			}
			// filterValidClaimKeys 통과 — 원본과 동일해야 함
			valid := filterValidClaimKeys(env.ClaimKeys)
			if !reflect.DeepEqual(valid, env.ClaimKeys) {
				t.Errorf("[%d] filterValidClaimKeys() = %v, want %v", i, valid, env.ClaimKeys)
			}
		}
	})

	t.Run("edge case envelope", func(t *testing.T) {
		t.Parallel()

		// alarm_queue_envelope_edge.json: single object
		data, err := os.ReadFile(filepath.Join(fixtureDir, "alarm_queue_envelope_edge.json"))
		if err != nil {
			t.Fatalf("fixture 읽기 실패: %v", err)
		}

		env, err := parseEnvelope(string(data))
		if err != nil {
			t.Fatalf("parseEnvelope() 오류 = %v", err)
		}
		// 버전 검증
		if env.Version != 1 {
			t.Errorf("Version = %d, want 1", env.Version)
		}
		// Channel, Stream 모두 nil (edge case)
		if env.Notification.Channel != nil {
			t.Errorf("Notification.Channel = %v, want nil", env.Notification.Channel)
		}
		if env.Notification.Stream != nil {
			t.Errorf("Notification.Stream = %v, want nil", env.Notification.Stream)
		}
		// ClaimKeys 빈 슬라이스
		if len(env.ClaimKeys) != 0 {
			t.Errorf("ClaimKeys = %v, want empty", env.ClaimKeys)
		}
		// Users 빈 슬라이스
		if len(env.Notification.Users) != 0 {
			t.Errorf("Notification.Users = %v, want empty", env.Notification.Users)
		}
	})
}
