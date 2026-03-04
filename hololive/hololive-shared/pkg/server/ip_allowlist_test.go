package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewIPAllowList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		wantLen  int
		wantErr  bool
	}{
		{
			name:    "유효한 CIDR 2개",
			input:   []string{"192.168.1.0/24", "10.0.0.0/8"},
			wantLen: 2,
			wantErr: false,
		},
		{
			name:    "단일 IPv4 주소는 /32 자동 추가",
			input:   []string{"192.168.1.1"},
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "단일 IPv6 주소는 /128 자동 추가",
			input:   []string{"::1"},
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "빈 문자열 필터링",
			input:   []string{"", "  ", "10.0.0.0/8"},
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "잘못된 CIDR → 에러 반환",
			input:   []string{"not-a-cidr"},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nets, err := NewIPAllowList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("에러 반환 예상이었으나 nil 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("예상치 못한 에러: %v", err)
			}
			if len(nets) != tt.wantLen {
				t.Fatalf("nets 길이 = %d, want %d", len(nets), tt.wantLen)
			}
		})
	}
}

func TestAdminIPAllowMiddleware(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		cidrs      []string // 허용 CIDR 목록 (빈 슬라이스 = 전체 허용)
		remoteAddr string   // 클라이언트 RemoteAddr
		wantStatus int
	}{
		{
			name:       "빈 허용 목록: 모든 IP 통과",
			cidrs:      []string{},
			remoteAddr: "1.2.3.4:9999",
			wantStatus: http.StatusOK,
		},
		{
			name:       "허용 대역 내 IP → 통과",
			cidrs:      []string{"192.168.1.0/24"},
			remoteAddr: "192.168.1.5:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "허용 대역 외 IP → 403",
			cidrs:      []string{"192.168.1.0/24"},
			remoteAddr: "10.0.0.1:12345",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nets, err := NewIPAllowList(tt.cidrs)
			if err != nil {
				t.Fatalf("NewIPAllowList 에러: %v", err)
			}

			router := gin.New()
			// TrustedProxies를 nil로 설정해 RemoteAddr를 ClientIP()로 직접 사용
			if err := router.SetTrustedProxies(nil); err != nil {
				t.Fatalf("SetTrustedProxies 에러: %v", err)
			}
			router.Use(AdminIPAllowMiddleware(nets, slog.Default()))
			router.GET("/admin", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			req.RemoteAddr = tt.remoteAddr
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (remoteAddr=%s)", rec.Code, tt.wantStatus, tt.remoteAddr)
			}
		})
	}
}
