// Package ua: 동적 User-Agent 생성기
// YouTube 스크래핑 시 봇 탐지를 회피하기 위해 실제 브라우저 분포 기반으로 UA를 생성
package ua

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// HeaderSnapshot: 요청 단위 원자적 브라우저 헤더 스냅샷
type HeaderSnapshot struct {
	UserAgent       string // User-Agent 값
	SecChUA         string // Sec-CH-UA (빈 문자열 = 비-Chromium, 헤더 미설정)
	SecChUAPlatform string // Sec-CH-UA-Platform (빈 문자열 = 미설정)
	Accept          string // Accept 헤더 값 (브라우저별 프로필 일치)
}

// Provider: User-Agent + Client Hints를 원자적으로 제공하는 인터페이스
type Provider interface {
	// Headers: 단일 호출로 UA + Client Hints를 원자적으로 반환
	Headers(ctx context.Context) HeaderSnapshot
}

// Strategy: UA 회전 전략
type Strategy int

const (
	// StrategyPerRequest: 매 요청마다 새 UA 생성 (비권장: 비정상 패턴으로 탐지될 수 있음)
	StrategyPerRequest Strategy = iota
	// StrategySessionTTL: 세션 단위로 UA 유지 후 TTL 만료 시 회전 (권장)
	StrategySessionTTL
)

// RotatingProvider: 가중치 기반 동적 UA 생성기
type RotatingProvider struct {
	mu sync.Mutex
	r  *rand.Rand

	strategy       Strategy
	ttl            time.Duration
	expires        time.Time
	cachedSnapshot HeaderSnapshot
}

// NewRotatingProvider: 새 UA Provider 생성
// strategy: UA 회전 전략 (StrategySessionTTL 권장)
// ttl: 세션 TTL (기본값: 45분, StrategySessionTTL에서만 사용)
func NewRotatingProvider(strategy Strategy, ttl time.Duration) *RotatingProvider {
	if ttl <= 0 {
		ttl = 45 * time.Minute
	}
	return &RotatingProvider{
		r:        newRand(),
		strategy: strategy,
		ttl:      ttl,
	}
}

// Headers: 현재 전략에 따라 원자적 헤더 스냅샷 반환
func (p *RotatingProvider) Headers(_ context.Context) HeaderSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.strategy == StrategyPerRequest {
		return p.generate()
	}

	// SessionTTL 전략: 캐시된 스냅샷 사용, 만료 시 새로 생성
	now := time.Now()
	if p.cachedSnapshot.UserAgent == "" || now.After(p.expires) {
		p.cachedSnapshot = p.generate()
		// TTL에 지터 적용 (80%~120%)하여 회전 패턴 고정 방지
		jitter := time.Duration(float64(p.ttl) * (0.8 + 0.4*p.r.Float64()))
		p.expires = now.Add(jitter)
	}
	return p.cachedSnapshot
}

// weighted: 가중치가 적용된 항목
type weighted[T any] struct {
	v T
	w int
}

// pickWeighted: 가중치 기반 랜덤 선택
func pickWeighted[T any](r *rand.Rand, items []weighted[T]) T {
	total := 0
	for _, it := range items {
		total += it.w
	}
	n := r.Intn(total)
	for _, it := range items {
		n -= it.w
		if n < 0 {
			return it.v
		}
	}
	return items[len(items)-1].v
}

// browser: 브라우저 종류
type browser int

const (
	brChrome browser = iota
	brEdge
	brFirefox
	brSafari
)

// os: 운영체제 종류
type os int

const (
	osWin10 os = iota
	osWin11
	osMac13
	osMac14
	osMac15
	osMac16
)

// GREASE 브랜드 후보 (Chromium GREASE 사양)
var greaseBrands = []string{
	"Not(A:Brand",
	"Not A;Brand",
	"Not_A Brand",
	"Not/A)Brand",
}

const (
	chromiumAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	defaultAccept  = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

// generate: 가중치 기반으로 새 HeaderSnapshot 생성
func (p *RotatingProvider) generate() HeaderSnapshot {
	// 대략적인 데스크톱 브라우저 점유율 기반 가중치
	// Chrome ~68%, Safari ~20%, Edge ~7%, Firefox ~5%
	b := pickWeighted(p.r, []weighted[browser]{
		{brChrome, 68},
		{brEdge, 7},
		{brFirefox, 5},
		{brSafari, 20},
	})

	var o os
	switch b {
	case brSafari:
		// Safari는 macOS에서만 사용
		o = pickWeighted(p.r, []weighted[os]{
			{osMac13, 25},
			{osMac14, 35},
			{osMac15, 25},
			{osMac16, 15},
		})
	default:
		// Chrome/Edge/Firefox는 Windows와 macOS 모두 지원
		o = pickWeighted(p.r, []weighted[os]{
			{osWin10, 45},
			{osWin11, 35},
			{osMac13, 5},
			{osMac14, 5},
			{osMac15, 5},
			{osMac16, 5},
		})
	}

	switch b {
	case brChrome:
		return p.genChromeSnapshot(o)
	case brEdge:
		return p.genEdgeSnapshot(o)
	case brFirefox:
		return genFirefoxSnapshot(p.r, o)
	case brSafari:
		return genSafariSnapshot(p.r, o)
	default:
		return p.genChromeSnapshot(o)
	}
}

// genChromeSnapshot: Chrome HeaderSnapshot 생성 (UA Reduction + Client Hints)
func (p *RotatingProvider) genChromeSnapshot(o os) HeaderSnapshot {
	major := randInt(p.r, 141, 145)
	// Chrome 107+ UA Reduction: build/patch=0 고정
	uaStr := fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36",
		osToken(o), major,
	)

	grease := greaseBrands[p.r.Intn(len(greaseBrands))]
	secChUA := fmt.Sprintf(`%q;v="8", "Chromium";v="%d", "Google Chrome";v="%d"`, grease, major, major)

	return HeaderSnapshot{
		UserAgent:       uaStr,
		SecChUA:         secChUA,
		SecChUAPlatform: osPlatform(o),
		Accept:          chromiumAccept,
	}
}

// genEdgeSnapshot: Edge HeaderSnapshot 생성 (UA Reduction + Client Hints)
func (p *RotatingProvider) genEdgeSnapshot(o os) HeaderSnapshot {
	major := randInt(p.r, 141, 145)
	// Edge도 UA Reduction 적용: build/patch=0 고정
	uaStr := fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36 Edg/%d.0.0.0",
		osToken(o), major, major,
	)

	grease := greaseBrands[p.r.Intn(len(greaseBrands))]
	secChUA := fmt.Sprintf(`%q;v="8", "Chromium";v="%d", "Microsoft Edge";v="%d"`, grease, major, major)

	return HeaderSnapshot{
		UserAgent:       uaStr,
		SecChUA:         secChUA,
		SecChUAPlatform: osPlatform(o),
		Accept:          chromiumAccept,
	}
}

// genFirefoxSnapshot: Firefox HeaderSnapshot 생성 (Client Hints 미지원)
func genFirefoxSnapshot(r *rand.Rand, o os) HeaderSnapshot {
	major := randInt(r, 132, 135)
	return HeaderSnapshot{
		UserAgent: fmt.Sprintf(
			"Mozilla/5.0 (%s; rv:%d.0) Gecko/20100101 Firefox/%d.0",
			osToken(o), major, major,
		),
		Accept: defaultAccept,
	}
}

// genSafariSnapshot: Safari HeaderSnapshot 생성 (Client Hints 미지원)
func genSafariSnapshot(r *rand.Rand, o os) HeaderSnapshot {
	ver := randInt(r, 17, 18)
	return HeaderSnapshot{
		UserAgent: fmt.Sprintf(
			"Mozilla/5.0 (%s) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%d.0 Safari/605.1.15",
			osToken(o), ver,
		),
		Accept: defaultAccept,
	}
}

// osToken: OS 토큰 문자열 반환
func osToken(o os) string {
	switch o {
	case osWin10:
		return "Windows NT 10.0; Win64; x64"
	case osWin11:
		// Windows 11도 내부적으로는 NT 10.0으로 보고됨
		return "Windows NT 10.0; Win64; x64"
	case osMac13:
		return "Macintosh; Intel Mac OS X 13_6"
	case osMac14:
		return "Macintosh; Intel Mac OS X 14_2"
	case osMac15:
		return "Macintosh; Intel Mac OS X 15_0"
	case osMac16:
		return "Macintosh; Intel Mac OS X 16_0"
	default:
		return "Windows NT 10.0; Win64; x64"
	}
}

// osPlatform: Sec-CH-UA-Platform 값 반환
func osPlatform(o os) string {
	switch o {
	case osWin10, osWin11:
		return `"Windows"`
	case osMac13, osMac14, osMac15, osMac16:
		return `"macOS"`
	default:
		return `"Windows"`
	}
}

// randInt: minVal~maxVal 범위의 랜덤 정수 반환
func randInt(r *rand.Rand, minVal, maxVal int) int {
	if maxVal <= minVal {
		return minVal
	}
	return minVal + r.Intn(maxVal-minVal+1)
}

// newRand: crypto/rand로 시드된 새 rand.Rand 생성
func newRand() *rand.Rand {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	seed := int64(binary.LittleEndian.Uint64(b[:]))
	return rand.New(rand.NewSource(seed))
}

// StaticProvider: 고정 UA를 반환하는 Provider (테스트용)
type StaticProvider struct {
	ua string
}

// NewStaticProvider: 고정 UA Provider 생성
func NewStaticProvider(ua string) *StaticProvider {
	return &StaticProvider{ua: ua}
}

// Headers: 고정된 UA로 HeaderSnapshot 반환 (CH 필드 빈 문자열)
func (p *StaticProvider) Headers(_ context.Context) HeaderSnapshot {
	return HeaderSnapshot{
		UserAgent: p.ua,
		Accept:    chromiumAccept,
	}
}
