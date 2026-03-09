// Package ssr: SSR 데이터 주입 및 SPA 서빙
package ssr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/docker"
)

// Config: SSR 서빙 설정
type Config struct {
	AssetsDir           string
	IndexPath           string
	FaviconPath         string
	CacheControlAssets  string
	CacheControlHTML    string
	CacheControlFavicon string
}

// DefaultConfig: 기본 SSR 설정
func DefaultConfig() Config {
	return Config{
		AssetsDir:           "./admin-ui/dist/assets",
		IndexPath:           "./admin-ui/dist/index.html",
		FaviconPath:         "./admin-ui/dist/favicon.svg",
		CacheControlAssets:  "public, max-age=31536000, immutable",
		CacheControlHTML:    "no-store, no-cache, must-revalidate",
		CacheControlFavicon: "public, max-age=86400",
	}
}

// SSRData: SSR을 위해 HTML에 주입할 데이터 구조체
// React 클라이언트에서 window.__SSR_DATA__로 접근
type SSRData struct {
	Docker     json.RawMessage `json:"docker,omitempty"`
	Containers json.RawMessage `json:"containers,omitempty"`
	// holo-specific 데이터는 프록시를 통해 가져옴
	Members  json.RawMessage `json:"members,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

// Injector: SSR 데이터를 HTML에 주입하는 서비스
type Injector struct {
	dockerSvc  *docker.Service
	holoBotURL string
	htmlCache  []byte
	httpClient *http.Client
	logger     *slog.Logger
}

// NewInjector: 새로운 SSR 데이터 인젝터 생성
func NewInjector(
	dockerSvc *docker.Service,
	holoBotURL string,
	logger *slog.Logger,
) *Injector {
	return &Injector{
		dockerSvc:  dockerSvc,
		holoBotURL: holoBotURL,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		logger: logger.With(slog.String("component", "ssr")),
	}
}

// LoadHTMLCache: 파일 시스템에서 index.html 파일을 캐시 (개발 모드용)
func (s *Injector) LoadHTMLCache(indexPath string) error {
	// Validate path to prevent directory traversal
	absPath, err := filepath.Abs(indexPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Clean the path to remove any .. components
	cleanPath := filepath.Clean(absPath)

	// Verify the file exists and is a regular file (not a symlink to outside)
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", cleanPath)
	}

	htmlData, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Errorf("read index.html: %w", err)
	}
	s.htmlCache = htmlData
	s.logger.Info("HTML cache loaded from file", slog.String("path", cleanPath), slog.Int("size", len(htmlData)))
	return nil
}

// LoadHTMLFromBytes: 임베디드 바이트에서 HTML 캐시 로드 (프로덕션 모드용)
func (s *Injector) LoadHTMLFromBytes(data []byte) {
	s.htmlCache = data
	s.logger.Info("HTML cache loaded from embedded", slog.Int("size", len(data)))
}

// HasHTMLCache: HTML 캐시 존재 여부
func (s *Injector) HasHTMLCache() bool {
	return len(s.htmlCache) > 0
}

// GetHTMLCache: 캐시된 HTML 반환
func (s *Injector) GetHTMLCache() []byte {
	return s.htmlCache
}

// InjectForPath: 요청 경로에 맞는 SSR 데이터를 HTML에 주입
// 인증되지 않은 요청에는 빈 데이터 반환
func (s *Injector) InjectForPath(ctx context.Context, path string, isAuthenticated bool, sessionCookie string) ([]byte, error) {
	if len(s.htmlCache) == 0 {
		return nil, nil
	}

	// 인증되지 않은 사용자에게는 SSR 데이터 주입 안함
	if !isAuthenticated {
		return s.htmlCache, nil
	}

	// SSR 대상 경로 확인 및 데이터 프리페칭
	ssrData, err := s.fetchDataForPath(ctx, path, sessionCookie)
	if err != nil {
		s.logger.Warn("SSR data fetch failed", slog.String("path", path), slog.Any("error", err))
		return s.htmlCache, nil
	}

	if ssrData == nil {
		return s.htmlCache, nil
	}

	return s.injectData(ssrData)
}

// fetchDataForPath: 경로에 맞는 데이터를 프리페칭
func (s *Injector) fetchDataForPath(ctx context.Context, path string, sessionCookie string) (*SSRData, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ssrData := &SSRData{}
	hasData := false

	// /dashboard/members - 멤버 목록 프리페칭 (hololive-bot 프록시)
	if strings.HasPrefix(path, "/dashboard/members") {
		if data := s.fetchFromHoloBot(timeoutCtx, "/api/holo/members", sessionCookie); data != nil {
			ssrData.Members = data
			hasData = true
		}
	}

	// /dashboard/settings - 설정 + Docker 상태 프리페칭
	if strings.HasPrefix(path, "/dashboard/settings") {
		// 설정 데이터 (hololive-bot 프록시)
		if data := s.fetchFromHoloBot(timeoutCtx, "/api/holo/settings", sessionCookie); data != nil {
			ssrData.Settings = data
			hasData = true
		}

		// Docker 상태 (로컬)
		if s.dockerSvc != nil {
			available := s.dockerSvc.Available(timeoutCtx)
			dockerData, _ := json.Marshal(map[string]any{
				"status":    "ok",
				"available": available,
			})
			ssrData.Docker = dockerData
			hasData = true

			if available {
				containers, err := s.dockerSvc.ListContainers(timeoutCtx)
				if err == nil {
					containerData, _ := json.Marshal(map[string]any{
						"status":     "ok",
						"containers": containers,
					})
					ssrData.Containers = containerData
				}
			}
		}
	}

	if !hasData {
		return nil, nil
	}

	return ssrData, nil
}

// fetchFromHoloBot: hololive-bot에서 데이터 가져오기
func (s *Injector) fetchFromHoloBot(ctx context.Context, endpoint, sessionCookie string) json.RawMessage {
	if s.holoBotURL == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.holoBotURL+endpoint, http.NoBody)
	if err != nil {
		return nil
	}

	// 세션 쿠키 전달
	if sessionCookie != "" {
		req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Debug("Holo bot fetch failed", slog.String("endpoint", endpoint), slog.Any("error", err))
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	return body
}

// injectData: HTML에 SSR 데이터 주입
// </head> 태그 앞에 <script> 태그로 window.__SSR_DATA__ 설정
func (s *Injector) injectData(data *SSRData) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return s.htmlCache, nil
	}

	// XSS 방어: script 종료 시퀀스를 유니코드 이스케이프로 변환
	// upstream JSON에 </script>가 포함되면 브라우저가 스크립트 블록을 조기 종료할 수 있음
	safeJSON := strings.ReplaceAll(string(jsonData), "</script>", `\u003c/script\u003e`)
	safeJSON = strings.ReplaceAll(safeJSON, "</Script>", `\u003c/Script\u003e`)
	safeJSON = strings.ReplaceAll(safeJSON, "</SCRIPT>", `\u003c/SCRIPT\u003e`)

	// 스크립트 주입: </head> 앞에 삽입
	script := []byte(`<script>window.__SSR_DATA__=` + safeJSON + `;</script>`)
	injectionPoint := []byte("</head>")

	return bytes.Replace(s.htmlCache, injectionPoint, append(script, injectionPoint...), 1), nil
}
