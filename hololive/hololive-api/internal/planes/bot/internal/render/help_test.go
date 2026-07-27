package render

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

const helpCardTestText = `홀로라이브 봇 명령어

[방송]
  !라이브 - 방송 중 목록
  !라이브 [멤버명] - 멤버 라이브 확인
  !예정 - 예정 방송 목록
  !예정 [멤버명] - 멤버 예정 방송
  !멤버 [이름] - 일주일 이내 방송 일정
  !방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
  !방송이력 경마 30 - 최근 30일 경마
  !방송기록 페코라 게임 - 멤버·타입 필터
  !방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
  타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
  !방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
  !멤버 - 전체 멤버 목록
  !정보 [멤버명] - 프로필 조회

[알람]
  !알람 추가 [멤버명]
  !알람 제거 [멤버명]
  !알람 목록
  !알람 초기화

[뉴스]
  !뉴스 - 주간 뉴스 요약
  !뉴스알림 켜기 / 끄기 / 상태

[행사]
  !행사 - 행사 알림 상태
  !행사 켜기 / 끄기

[기념일]
  !기념일 - 이번 달 생일·주년
  !기념일 다음달 / 저번달

[기타]
  !구독자 [멤버명] - 구독자 수
  !도움말 - 도움말`

func TestHelpCardRendererUsesQuestionHelpVisualContract(t *testing.T) {
	renderer := NewHelpCardRenderer()
	images, err := renderer.RenderHelpImages(t.Context(), helpCardTestText)
	if err != nil {
		t.Fatalf("RenderHelpImages() error = %v", err)
	}
	if len(images) != 3 {
		t.Fatalf("image count = %d, want 3", len(images))
	}

	originalFirst := bytes.Clone(images[0])
	for index, imageData := range images {
		config, err := png.DecodeConfig(bytes.NewReader(imageData))
		if err != nil {
			t.Fatalf("decode PNG %d config: %v", index+1, err)
		}
		if config.Width != helpCardOutputWidth {
			t.Fatalf("PNG %d width = %d, want %d", index+1, config.Width, helpCardOutputWidth)
		}
		if config.Height < helpCardMinHeight || config.Height > helpCardMaxHeight {
			t.Fatalf("PNG %d height = %d, want %d..%d", index+1, config.Height, helpCardMinHeight, helpCardMaxHeight)
		}
		if len(imageData) > helpCardMaxPNGBytes {
			t.Fatalf("PNG %d size = %d, want <= %d", index+1, len(imageData), helpCardMaxPNGBytes)
		}
	}

	decoded, err := png.Decode(bytes.NewReader(originalFirst))
	if err != nil {
		t.Fatalf("decode first PNG: %v", err)
	}
	if got := color.RGBAModel.Convert(decoded.At(0, 0)).(color.RGBA); got != helpColorBackgroundStart {
		t.Fatalf("background pixel = %#v, want %#v", got, helpColorBackgroundStart)
	}
	if got := color.RGBAModel.Convert(decoded.At(100, 200)).(color.RGBA); got != helpColorPanel {
		t.Fatalf("panel pixel = %#v, want %#v", got, helpColorPanel)
	}

	images[0][0] ^= 0xff
	cached, err := renderer.RenderHelpImages(t.Context(), helpCardTestText)
	if err != nil {
		t.Fatalf("cached RenderHelpImages() error = %v", err)
	}
	if !bytes.Equal(cached[0], originalFirst) {
		t.Fatal("cached PNG was aliased by the caller")
	}
}

func TestHelpCardPaginationPreservesEveryCommand(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		t.Fatalf("loadHelpCardFonts() error = %v", err)
	}
	document, err := parseHelpCardDocument(t.Context(), helpCardTestText, faces)
	if err != nil {
		t.Fatalf("parseHelpCardDocument() error = %v", err)
	}
	pages, err := paginateHelpDocument(document)
	if err != nil {
		t.Fatalf("paginateHelpDocument() error = %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("page count = %d, want 3", len(pages))
	}

	wantRows := helpDocumentRowCount(document.sections)
	gotRows := 0
	for _, page := range pages {
		gotRows += helpDocumentRowCount(page.sections)
	}
	if gotRows != wantRows {
		t.Fatalf("paginated rows = %d, want %d", gotRows, wantRows)
	}
}

func TestHelpCardRendererRejectsCanceledAndOversizedInput(t *testing.T) {
	renderer := NewHelpCardRenderer()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := renderer.RenderHelpImages(ctx, "도움말\n!도움말"); err == nil {
		t.Fatal("RenderHelpImages() accepted a canceled context")
	}

	oversized := "도움말\n" + strings.Repeat("가", helpCardMaxTextBytes)
	if _, err := renderer.RenderHelpImages(t.Context(), oversized); err == nil {
		t.Fatal("RenderHelpImages() accepted oversized text")
	}
}

func TestWrapHelpLineKeepsEveryRune(t *testing.T) {
	fontMu.Lock()
	defer fontMu.Unlock()

	faces, err := loadHelpCardFonts()
	if err != nil {
		t.Fatalf("loadHelpCardFonts() error = %v", err)
	}

	text := "방송이력 카테고리 게임 기간 개수 필터를 함께 사용할 수 있습니다"
	maxWidth := cardkit.MeasureText(faces.description, "방송이력 카테고리 게임")
	lines := wrapHelpLine(faces.description, text, maxWidth)
	if len(lines) < 2 {
		t.Fatalf("wrapped line count = %d, want at least 2", len(lines))
	}
	if strings.Join(lines, " ") != text {
		t.Fatalf("wrapped text = %q, want %q", strings.Join(lines, " "), text)
	}
}

func helpDocumentRowCount(sections []helpCardSection) int {
	count := 0
	for _, section := range sections {
		count += len(section.rows)
	}
	return count
}
