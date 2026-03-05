package twitch

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
)

// newTestLogger: 테스트용 무음 slog 로거를 반환합니다.
var newTestLogger = sharedlogging.NewLogger

// newTestClient: 테스트용 Client를 생성합니다.
func newTestClient(clientID, clientSecret string) *Client {
	return NewClient(ClientConfig{ClientID: clientID, ClientSecret: clientSecret}, newTestLogger())
}

func TestClient_IsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client *Client
		want   bool
	}{
		{
			name:   "nil 클라이언트는 false",
			client: nil,
			want:   false,
		},
		{
			name:   "clientID 미설정 시 false",
			client: newTestClient("", "secret"),
			want:   false,
		},
		{
			name:   "clientSecret 미설정 시 false",
			client: newTestClient("id", ""),
			want:   false,
		},
		{
			name:   "둘 다 설정 시 true",
			client: newTestClient("id", "secret"),
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.client.IsConfigured()
			if got != tc.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStreamData_IsLive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stream StreamData
		want   bool
	}{
		{
			name:   "Type=live 이면 true",
			stream: StreamData{Type: "live"},
			want:   true,
		},
		{
			name:   "Type 빈 문자열이면 false",
			stream: StreamData{Type: ""},
			want:   false,
		},
		{
			name:   "Type=offline 이면 false",
			stream: StreamData{Type: "offline"},
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.stream.IsLive()
			if got != tc.want {
				t.Errorf("IsLive() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStreamData_GetThumbnailURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stream StreamData
		width  int
		height int
		want   string
	}{
		{
			name:   "{width}와 {height} 플레이스홀더를 올바르게 치환",
			stream: StreamData{ThumbnailURL: "https://example.com/thumb-{width}x{height}.jpg"},
			width:  320,
			height: 180,
			want:   "https://example.com/thumb-320x180.jpg",
		},
		{
			name:   "플레이스홀더 없는 URL은 그대로 반환",
			stream: StreamData{ThumbnailURL: "https://example.com/thumb.jpg"},
			width:  320,
			height: 180,
			want:   "https://example.com/thumb.jpg",
		},
		{
			name:   "빈 URL은 빈 문자열 반환",
			stream: StreamData{ThumbnailURL: ""},
			width:  320,
			height: 180,
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.stream.GetThumbnailURL(tc.width, tc.height)
			if got != tc.want {
				t.Errorf("GetThumbnailURL(%d, %d) = %q, want %q", tc.width, tc.height, got, tc.want)
			}
		})
	}
}

func TestClient_CircuitBreaker(t *testing.T) {
	t.Parallel()

	// FailureThreshold = 3: 3회 실패 시 Circuit OPEN
	threshold := constants.CircuitBreakerConfig.FailureThreshold

	t.Run("recordFailure를 threshold회 호출하면 Circuit이 열림", func(t *testing.T) {
		t.Parallel()

		c := newTestClient("id", "secret")

		for i := range threshold {
			// threshold 도달 전에는 열리지 않아야 함
			if i < threshold-1 {
				if c.IsCircuitOpen() {
					t.Errorf("실패 %d회 후 Circuit이 열렸으나 아직 열리면 안 됨 (threshold=%d)", i+1, threshold)
				}
			}
			c.recordFailure()
		}

		if !c.IsCircuitOpen() {
			t.Errorf("실패 %d회 후 Circuit이 열려야 하지만 열리지 않음", threshold)
		}
	})

	t.Run("recordSuccess는 실패 카운트를 초기화함", func(t *testing.T) {
		t.Parallel()

		c := newTestClient("id", "secret")

		// threshold - 1 회 실패 누적
		for range threshold - 1 {
			c.recordFailure()
		}

		// 성공 시 카운터 리셋
		c.recordSuccess()

		// 다시 threshold - 1 회 실패해도 Circuit이 열리면 안 됨
		for range threshold - 1 {
			c.recordFailure()
		}

		if c.IsCircuitOpen() {
			t.Error("recordSuccess 후 카운터가 초기화되어야 하므로 Circuit이 열리면 안 됨")
		}
	})
}
