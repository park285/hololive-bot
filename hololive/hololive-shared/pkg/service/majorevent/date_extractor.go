package majorevent

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

var (
	japaneseDatePattern      = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	slashDatePattern         = regexp.MustCompile(`(\d{4})[/\-](\d{1,2})[/\-](\d{1,2})`)
	dotDatePattern           = regexp.MustCompile(`(\d{4})\.\s*(\d{1,2})\.(\d{1,2})`)
	shortJapaneseDatePattern = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
	// 멀티데이: 2026年3月6日～8日, 2026年3月6日（金）～8日（日）, 2026年3月6日～3月8日
	// NFKC 정규화 후 U+FF5E(～)→U+007E(~)로 변환되므로 ASCII ~ 포함
	multiDayRangePattern = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日(?:\([^)]*\)|（[^）]*）)?\s*[～〜~\-]\s*(?:(\d{1,2})月)?(\d{1,2})日`)
	htmlTagPattern       = regexp.MustCompile(`<[^>]+>`)
	scriptPattern        = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	stylePattern         = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	// 섹션 헤더 패턴 (h4-h6)
	sectionHeaderPattern = regexp.MustCompile(`(?i)<h[456][^>]*>(.*?)</h[456]>`)
	kst                  = time.FixedZone("KST", 9*60*60)
)

const (
	clusterGap         = 150
	maxContextDistance = 150
	tieThreshold       = 10
)

var (
	positiveKeywords = []string{
		"開催", "日時", "日程", "公演", "開演", "会期", "期間",
		"ライブ", "コンサート", "開場", "ステージ",
	}
	negativeKeywords = []string{
		"チケット", "先行", "抽選", "受付", "販売", "発売",
		"申込", "締切", "アーカイブ", "予約", "募集", "応募",
		"視聴期限", "購入", "配信期間",
	}
	// 강력 긍정 키워드: 섹션 헤더 보너스 판별에 사용
	strongPositiveKeywords = []string{
		"開催日時", "開催日程", "公演日時",
	}
)

// headerInfo: HTML에서 추출한 헤더 정보
type headerInfo struct {
	text string // 헤더 내 텍스트 (例: "開催日時", "チケット")
}

// sectionRange: plain text 좌표계 기반 섹션 범위
type sectionRange struct {
	id       int
	startPos int    // plain text 기준: 헤더 텍스트 끝 위치
	endPos   int    // plain text 기준: 다음 헤더 시작 전 (또는 EOF)
	header   string // 헤더 텍스트 (키워드 판별용)
}

type dateMatch struct {
	date     time.Time
	position int
	raw      string
	score    int
}

type DateExtractor struct{}

func NewDateExtractor() *DateExtractor {
	return &DateExtractor{}
}

func (e *DateExtractor) Extract(html string) []time.Time {
	dates := make([]time.Time, 0)
	seen := make(map[string]bool)

	matches := japaneseDatePattern.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if t, ok := e.parseYMD(m[1], m[2], m[3]); ok {
			key := t.Format("2006-01-02")
			if !seen[key] {
				dates = append(dates, t)
				seen[key] = true
			}
		}
	}

	matches = slashDatePattern.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if t, ok := e.parseYMD(m[1], m[2], m[3]); ok {
			key := t.Format("2006-01-02")
			if !seen[key] {
				dates = append(dates, t)
				seen[key] = true
			}
		}
	}

	matches = dotDatePattern.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if t, ok := e.parseYMD(m[1], m[2], m[3]); ok {
			key := t.Format("2006-01-02")
			if !seen[key] {
				dates = append(dates, t)
				seen[key] = true
			}
		}
	}

	return dates
}

func (e *DateExtractor) ExtractEventDates(html string) []time.Time {
	// Unicode NFKC 정규화: 호환 문자(例: ⽇ U+2F47 → 日 U+65E5) 통일
	html = norm.NFKC.String(html)

	// Step 1: HTML에서 헤더 추출 (stripHTML 전)
	headers := extractHeaders(html)

	plain := e.stripHTML(html)

	// Step 2: plain text에서 섹션 매핑
	sections := buildSectionMap(plain, headers)

	allMatches := e.extractAllMatches(plain)
	if len(allMatches) == 0 {
		return nil
	}

	// 섹션 ID 할당 + context score 계산
	for i := range allMatches {
		sectionID := findSectionForPosition(sections, allMatches[i].position)
		if sectionID >= 0 && sectionID < len(sections) {
			// 같은 섹션 내 키워드만 거리 계산
			section := sections[sectionID]
			sectionText := plain[section.startPos:section.endPos]
			allMatches[i].score = e.calculateSectionContextScore(sectionText, allMatches[i].position-section.startPos)
			// 섹션 헤더 보너스
			allMatches[i].score += sectionHeaderBonus(section.header)
		} else {
			// 섹션 미할당: 기존 거리 기반 scoring fallback
			allMatches[i].score = e.calculateContextScore(plain, allMatches[i].position)
		}
	}

	positiveMatches := e.filterPositiveMatches(allMatches)
	selectedMatches := positiveMatches
	if len(positiveMatches) > 0 {
		selectedMatches = e.selectBestCluster(positiveMatches)
	}
	if len(selectedMatches) == 0 {
		// 티켓/아카이브 관련 날짜(score<0)는 가능한 한 제외하고 이벤트 본문 날짜를 우선합니다.
		nonNegative := e.filterNonNegativeMatches(allMatches)
		if len(nonNegative) > 0 {
			selectedMatches = e.selectBestCluster(nonNegative)
		} else {
			selectedMatches = e.selectBestCluster(allMatches)
		}
	}

	dates := e.extractUniqueDates(selectedMatches)
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})

	// 지난 날짜라도 실제 행사일이면 유지해야 잘못된 "미래 아카이브 날짜" 치환을 방지할 수 있습니다.
	return dates
}

// extractHeaders: HTML에서 h4-h6 헤더 텍스트를 추출합니다 (stripHTML 전 호출).
// 내부 태그(<span>等)를 제거하고 순수 텍스트만 반환합니다.
func extractHeaders(html string) []headerInfo {
	matches := sectionHeaderPattern.FindAllStringSubmatch(html, -1)
	headers := make([]headerInfo, 0, len(matches))
	for _, m := range matches {
		// 내부 태그 제거 후 텍스트만 추출
		text := strings.TrimSpace(htmlTagPattern.ReplaceAllString(m[1], ""))
		if text != "" {
			headers = append(headers, headerInfo{text: text})
		}
	}
	return headers
}

// buildSectionMap: plain text에서 헤더 텍스트를 찾아 섹션 범위를 생성합니다.
// 탐색 규칙: lastIndex 이후 first-match만 허용, 미탐색 시 비활성.
func buildSectionMap(plainText string, headers []headerInfo) []sectionRange {
	sections := make([]sectionRange, 0, len(headers))
	lastIndex := 0

	type mappedHeader struct {
		text     string
		startPos int // 헤더 텍스트 시작 위치
		endPos   int // 헤더 텍스트 끝 위치
	}

	mapped := make([]mappedHeader, 0, len(headers))
	for _, h := range headers {
		idx := strings.Index(plainText[lastIndex:], h.text)
		if idx == -1 {
			// 매핑 실패: 해당 헤더 비활성
			continue
		}
		absPos := lastIndex + idx
		mapped = append(mapped, mappedHeader{
			text:     h.text,
			startPos: absPos,
			endPos:   absPos + len(h.text),
		})
		lastIndex = absPos + len(h.text)
	}

	for i, mh := range mapped {
		endPos := len(plainText)
		if i+1 < len(mapped) {
			endPos = mapped[i+1].startPos
		}
		sections = append(sections, sectionRange{
			id:       i,
			startPos: mh.endPos,
			endPos:   endPos,
			header:   mh.text,
		})
	}

	return sections
}

// findSectionForPosition: 주어진 position이 속하는 섹션 ID를 반환합니다.
// 섹션에 속하지 않으면 -1 반환.
func findSectionForPosition(sections []sectionRange, pos int) int {
	for _, s := range sections {
		if pos >= s.startPos && pos < s.endPos {
			return s.id
		}
	}
	return -1
}

// sectionHeaderBonus: 섹션 헤더가 strong positive/negative 키워드를 포함하면 보너스/페널티 반환.
func sectionHeaderBonus(header string) int {
	headerLower := strings.ToLower(header)
	for _, kw := range strongPositiveKeywords {
		if strings.Contains(headerLower, strings.ToLower(kw)) {
			return 5
		}
	}
	for _, kw := range negativeKeywords {
		if strings.Contains(headerLower, strings.ToLower(kw)) {
			return -5
		}
	}
	return 0
}

func (e *DateExtractor) stripHTML(html string) string {
	plain := scriptPattern.ReplaceAllString(html, " ")
	plain = stylePattern.ReplaceAllString(plain, " ")
	plain = htmlTagPattern.ReplaceAllString(plain, " ")
	plain = strings.ReplaceAll(plain, "&nbsp;", " ")
	plain = strings.ReplaceAll(plain, "&amp;", "&")
	plain = strings.ReplaceAll(plain, "&lt;", "<")
	plain = strings.ReplaceAll(plain, "&gt;", ">")
	return plain
}

func (e *DateExtractor) extractAllMatches(plain string) []dateMatch {
	matches := make([]dateMatch, 0)
	seen := make(map[string]bool)

	multiMatches := multiDayRangePattern.FindAllStringSubmatchIndex(plain, -1)
	for _, idx := range multiMatches {
		raw := plain[idx[0]:idx[1]]
		year, _ := strconv.Atoi(plain[idx[2]:idx[3]])
		startMonth, _ := strconv.Atoi(plain[idx[4]:idx[5]])
		startDay, _ := strconv.Atoi(plain[idx[6]:idx[7]])

		endMonth := startMonth
		if idx[8] != -1 && idx[9] != -1 {
			endMonth, _ = strconv.Atoi(plain[idx[8]:idx[9]])
		}
		endDay, _ := strconv.Atoi(plain[idx[10]:idx[11]])

		if !e.isValidDate(startMonth, startDay) || !e.isValidDate(endMonth, endDay) {
			continue
		}

		endYear := year
		if endMonth < startMonth {
			endYear = year + 1
		}

		startDate := time.Date(year, time.Month(startMonth), startDay, 0, 0, 0, 0, kst)
		endDate := time.Date(endYear, time.Month(endMonth), endDay, 0, 0, 0, 0, kst)

		for _, d := range []time.Time{startDate, endDate} {
			key := e.makeDedupeKey(d, idx[0])
			if !seen[key] {
				matches = append(matches, dateMatch{date: d, position: idx[0], raw: raw})
				seen[key] = true
			}
		}
	}

	// 단일 날짜 패턴 매칭 (YYYY年M月D日, YYYY/M/D, YYYY.M.D)
	e.collectYMDMatches(plain, japaneseDatePattern, multiMatches, seen, &matches)
	e.collectYMDMatches(plain, slashDatePattern, nil, seen, &matches)
	e.collectYMDMatches(plain, dotDatePattern, nil, seen, &matches)

	return matches
}

// collectYMDMatches: YYYY/M/D 패턴 매칭 결과를 수집합니다. skipOverlap이 non-nil이면 해당 범위 내 위치를 건너뜁니다.
func (e *DateExtractor) collectYMDMatches(plain string, pattern *regexp.Regexp, skipOverlap [][]int, seen map[string]bool, matches *[]dateMatch) {
	for _, idx := range pattern.FindAllStringSubmatchIndex(plain, -1) {
		if skipOverlap != nil && e.isPositionCovered(idx[0], skipOverlap) {
			continue
		}
		raw := plain[idx[0]:idx[1]]
		if t, ok := e.parseYMD(plain[idx[2]:idx[3]], plain[idx[4]:idx[5]], plain[idx[6]:idx[7]]); ok {
			key := e.makeDedupeKey(t, idx[0])
			if !seen[key] {
				*matches = append(*matches, dateMatch{date: t, position: idx[0], raw: raw})
				seen[key] = true
			}
		}
	}
}

func (e *DateExtractor) makeDedupeKey(d time.Time, pos int) string {
	return d.Format("2006-01-02") + "@" + strconv.Itoa(pos)
}

func (e *DateExtractor) isPositionCovered(pos int, multiMatches [][]int) bool {
	for _, idx := range multiMatches {
		if pos >= idx[0] && pos < idx[1] {
			return true
		}
	}
	return false
}

func (e *DateExtractor) isValidDate(month, day int) bool {
	return month >= 1 && month <= 12 && day >= 1 && day <= 31
}

func (e *DateExtractor) calculateContextScore(plain string, position int) int {
	plainLower := strings.ToLower(plain)
	posDistance := e.findNearestKeywordDistance(plainLower, position, positiveKeywords)
	negDistance := e.findNearestKeywordDistance(plainLower, position, negativeKeywords)

	if posDistance > maxContextDistance {
		posDistance = -1
	}
	if negDistance > maxContextDistance {
		negDistance = -1
	}

	if posDistance == -1 && negDistance == -1 {
		return 0
	}

	if posDistance == -1 {
		return -3
	}
	if negDistance == -1 {
		return 2
	}

	diff := posDistance - negDistance
	if diff < 0 {
		diff = -diff
	}
	if diff <= tieThreshold {
		return 0
	}

	if posDistance < negDistance {
		return 2
	}
	return -3
}

// calculateSectionContextScore: 섹션 범위 내에서만 키워드 거리 기반 scoring
func (e *DateExtractor) calculateSectionContextScore(sectionText string, relativePos int) int {
	textLower := strings.ToLower(sectionText)
	posDistance := e.findNearestKeywordDistance(textLower, relativePos, positiveKeywords)
	negDistance := e.findNearestKeywordDistance(textLower, relativePos, negativeKeywords)

	if posDistance > maxContextDistance {
		posDistance = -1
	}
	if negDistance > maxContextDistance {
		negDistance = -1
	}

	if posDistance == -1 && negDistance == -1 {
		return 0
	}

	if posDistance == -1 {
		return -3
	}
	if negDistance == -1 {
		return 2
	}

	diff := posDistance - negDistance
	if diff < 0 {
		diff = -diff
	}
	if diff <= tieThreshold {
		return 0
	}

	if posDistance < negDistance {
		return 2
	}
	return -3
}

func (e *DateExtractor) findNearestKeywordDistance(plainLower string, position int, keywords []string) int {
	minDistance := -1

	for _, kw := range keywords {
		kwLower := strings.ToLower(kw)
		idx := 0
		for {
			found := strings.Index(plainLower[idx:], kwLower)
			if found == -1 {
				break
			}
			kwPos := idx + found
			distance := position - kwPos
			if distance < 0 {
				distance = -distance
			}
			if minDistance == -1 || distance < minDistance {
				minDistance = distance
			}
			idx = kwPos + len(kwLower)
		}
	}
	return minDistance
}

func (e *DateExtractor) filterPositiveMatches(matches []dateMatch) []dateMatch {
	result := make([]dateMatch, 0)
	for _, m := range matches {
		if m.score > 0 {
			result = append(result, m)
		}
	}
	return result
}

func (e *DateExtractor) filterNonNegativeMatches(matches []dateMatch) []dateMatch {
	result := make([]dateMatch, 0, len(matches))
	for _, m := range matches {
		if m.score >= 0 {
			result = append(result, m)
		}
	}
	return result
}

func (e *DateExtractor) selectBestCluster(matches []dateMatch) []dateMatch {
	if len(matches) == 0 {
		return nil
	}

	sorted := make([]dateMatch, len(matches))
	copy(sorted, matches)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].position < sorted[j].position
	})

	clusters := make([][]dateMatch, 0)
	currentCluster := []dateMatch{sorted[0]}

	for i := 1; i < len(sorted); i++ {
		if sorted[i].position-sorted[i-1].position <= clusterGap {
			currentCluster = append(currentCluster, sorted[i])
		} else {
			clusters = append(clusters, currentCluster)
			currentCluster = []dateMatch{sorted[i]}
		}
	}
	clusters = append(clusters, currentCluster)

	bestIdx := 0
	bestScore := e.clusterScore(clusters[0])
	for i := 1; i < len(clusters); i++ {
		score := e.clusterScore(clusters[i])
		if score > bestScore || (score == bestScore && len(clusters[i]) > len(clusters[bestIdx])) {
			bestScore = score
			bestIdx = i
		}
	}

	return clusters[bestIdx]
}

func (e *DateExtractor) clusterScore(cluster []dateMatch) int {
	total := 0
	for _, m := range cluster {
		total += m.score
	}
	return total
}

func (e *DateExtractor) extractUniqueDates(matches []dateMatch) []time.Time {
	seen := make(map[string]bool)
	dates := make([]time.Time, 0, len(matches))
	for _, m := range matches {
		key := m.date.Format("2006-01-02")
		if !seen[key] {
			dates = append(dates, m.date)
			seen[key] = true
		}
	}
	return dates
}

// ExtractWithContext: 연도 컨텍스트를 사용하여 M月D日 형식도 추출합니다.
func (e *DateExtractor) ExtractWithContext(html string, contextYear int) []time.Time {
	dates := e.Extract(html)
	seen := make(map[string]bool)
	for _, d := range dates {
		seen[d.Format("2006-01-02")] = true
	}

	// M月D日 형식 추출 (연도 없는 경우)
	matches := shortJapaneseDatePattern.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])

		if month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			t := time.Date(contextYear, time.Month(month), day, 0, 0, 0, 0, kst)
			key := t.Format("2006-01-02")
			if !seen[key] {
				dates = append(dates, t)
				seen[key] = true
			}
		}
	}

	return dates
}

func (e *DateExtractor) parseYMD(yearStr, monthStr, dayStr string) (time.Time, bool) {
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return time.Time{}, false
	}
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(dayStr)
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, false
	}

	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, kst)
	if t.Month() != time.Month(month) || t.Day() != day {
		return time.Time{}, false
	}

	return t, true
}
