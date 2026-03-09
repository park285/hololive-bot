// Package static: 프론트엔드 정적 파일 임베딩
//
// go embed 지시자를 사용하여 빌드 시점에 React 빌드 결과물을 바이너리에 포함합니다.
// 단일 실행 파일 배포를 가능하게 하고 파일 시스템 의존을 제거합니다.
//
// 빌드 시 Dockerfile에서 frontend/dist 내용을 admin-ui/dist로 복사해야 합니다.
package static

import (
	"embed"
	"fmt"
	"io/fs"
)

// 프론트엔드 빌드 결과물을 임베딩합니다.
// Dockerfile에서 frontend/dist → backend/internal/static/admin-ui/dist로 복사됨
//
//go:embed all:admin-ui/dist
var distFS embed.FS

// Assets: 임베딩된 정적 파일의 assets 서브디렉토리를 반환합니다.
// /assets/* 경로에서 서빙됩니다.
func Assets() (fs.FS, error) {
	assetsFS, err := fs.Sub(distFS, "admin-ui/dist/assets")
	if err != nil {
		return nil, fmt.Errorf("sub assets fs: %w", err)
	}
	return assetsFS, nil
}

// IndexHTML: index.html 파일 내용을 반환합니다.
func IndexHTML() ([]byte, error) {
	data, err := distFS.ReadFile("admin-ui/dist/index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}
	return data, nil
}

// Favicon: favicon.svg 파일 내용을 반환합니다.
func Favicon() ([]byte, error) {
	data, err := distFS.ReadFile("admin-ui/dist/favicon.svg")
	if err != nil {
		return nil, fmt.Errorf("read favicon.svg: %w", err)
	}
	return data, nil
}

// FS: 전체 dist 디렉토리의 파일 시스템을 반환합니다.
func FS() (fs.FS, error) {
	dist, err := fs.Sub(distFS, "admin-ui/dist")
	if err != nil {
		return nil, fmt.Errorf("sub dist fs: %w", err)
	}
	return dist, nil
}

// HasEmbedded: 임베딩된 파일이 존재하는지 확인합니다.
func HasEmbedded() bool {
	_, err := distFS.ReadFile("admin-ui/dist/index.html")
	return err == nil
}

// CriticalAssets: index.html에서 critical CSS/JS 경로를 추출합니다.
// Early Hints (103)에서 preload 힌트로 사용됩니다.
func CriticalAssets() (css, js []string) {
	data, err := distFS.ReadFile("admin-ui/dist/index.html")
	if err != nil {
		return nil, nil
	}

	html := string(data)

	// CSS: <link rel="stylesheet" href="/assets/index-xxx.css">
	cssStart := 0
	for {
		idx := findSubstring(html[cssStart:], `href="/assets/`)
		if idx == -1 {
			break
		}
		idx += cssStart
		start := idx + len(`href="`)
		end := findSubstring(html[start:], `"`)
		if end == -1 {
			break
		}
		end += start
		path := html[start:end]
		if isCSS(path) {
			css = append(css, path)
		}
		cssStart = end
	}

	// JS: <script type="module" src="/assets/index-xxx.js">
	jsStart := 0
	for {
		idx := findSubstring(html[jsStart:], `src="/assets/`)
		if idx == -1 {
			break
		}
		idx += jsStart
		start := idx + len(`src="`)
		end := findSubstring(html[start:], `"`)
		if end == -1 {
			break
		}
		end += start
		path := html[start:end]
		if isJS(path) {
			js = append(js, path)
		}
		jsStart = end
	}

	return css, js
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func isCSS(path string) bool {
	return len(path) > 4 && path[len(path)-4:] == ".css"
}

func isJS(path string) bool {
	return len(path) > 3 && path[len(path)-3:] == ".js"
}
