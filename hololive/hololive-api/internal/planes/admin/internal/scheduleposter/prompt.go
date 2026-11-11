package scheduleposter

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const glossaryOrg = "Hololive"

const (
	tableSeparator    = "⇒"
	maxTableLines     = 80
	maxTableLineRunes = 200
	textPromptBase    = `이 VTuber 주간 일정 포스터에 보이는 일본어 글자를 모두 읽어 한국어 번역표를 만들어라.
- 한 줄에 하나씩 "원문 ⇒ 번역" 형식으로만 출력하고, 설명·머리말·맺음말을 쓰지 않는다.
- 포스터의 위에서 아래, 왼쪽에서 오른쪽 순서로 적는다. 같은 문구가 여러 번 나오면 한 번만 적는다.
- 로마자로만 된 글자(영어 제목, 영어 요일, 로마자 이름, @계정명)와 숫자로만 된 날짜·시각은 번역표에 넣지 않는다.
- 사람 이름은 아래 이름 대응표의 한국어 표기를 그대로 쓴다. 장식 글꼴이나 가나 섞임 때문에 원문이 대응표와 한두 글자 달라 보여도 가장 가까운 항목을 쓴다. 이름의 한자를 뜻으로 풀어 옮기지 않는다.
- 용어 대응표에 있는 말은 그 표기를 쓴다.
- 대응표에 없는 고유 명사(게임, 기획, 유닛, 방송 코너 이름)는 Google 검색으로 한국 팬들이 쓰는 표기를 확인해 쓰고, 확인되지 않으면 일본어 발음대로 한글로 적는다.
- 방송 제목은 한국 VTuber 팬들이 쓰는 말투와 용어(콜라보, 멤버십 한정, 잡담 등)로 자연스럽게 옮긴다. 말장난은 뜻과 말맛을 함께 살리고, 원문에 없는 내용을 더하거나 빼지 않는다.`
	imagePromptBase = `이 VTuber 주간 일정 포스터의 일본어 글자를 아래 번역표의 한국어로 바꾼 같은 포스터 이미지를 만들어라.
- 번역표의 원문이 있던 자리에 번역 문자열을 정확히 그린다. 번역 문자열의 글자를 고치거나 줄이거나 덧붙이지 않는다.
- 번역표에 없는 일본어 글자가 남아 있으면 같은 말투로 자연스러운 한국어로 옮긴다.
- 영어 요일(MON~SUN)은 월·화·수·목·금·토·일로 바꾼다. 그 밖의 로마자 글자(영어 제목, 로마자 이름, @계정명)와 숫자(날짜, 시각)는 한 글자도 바꾸지 않는다.
- 배치, 캐릭터 그림, 배경, 색, 장식, 이미지 비율은 그대로 둔다. 한글은 원문 글자와 같은 크기·굵기·색·외곽선과 비슷한 글꼴 분위기로 또렷하게 그리고, 칸을 넘치면 글자 크기만 줄인다.
- 원문에 없는 글자나 장식을 더하지 않는다.

번역표:
`
)

type memberSource interface {
	GetAllMembers(ctx context.Context) ([]*domain.Member, error)
}

// buildTextPrompt는 포스터 주인 멤버를 대응표보다 먼저 적어 다른 멤버 이름과 섞이지 않게 한다.
func buildTextPrompt(channelID string, members []*domain.Member, glossary map[string]string) string {
	var prompt strings.Builder

	prompt.WriteString(textPromptBase)

	named := make([]*domain.Member, 0, len(members))

	for _, member := range members {
		if member == nil || member.NameJa == "" || member.NameKo == "" {
			continue
		}

		if member.ChannelID == channelID {
			fmt.Fprintf(&prompt, "\n\n이 포스터의 멤버: %s", memberLine(member))
		}

		if !member.IsGraduated && member.GetOrg() == glossaryOrg {
			named = append(named, member)
		}
	}

	slices.SortFunc(named, func(a, b *domain.Member) int { return cmp.Compare(a.NameJa, b.NameJa) })

	if len(named) > 0 {
		prompt.WriteString("\n\n이름 대응표 (일본어 (로마자) → 한국어):")

		for _, member := range named {
			prompt.WriteString("\n" + memberLine(member))
		}
	}

	if len(glossary) > 0 {
		prompt.WriteString("\n\n용어 대응표 (원문 → 한국어):")

		for _, term := range slices.Sorted(maps.Keys(glossary)) {
			prompt.WriteString("\n" + term + " → " + glossary[term])
		}
	}

	return prompt.String()
}

func memberLine(member *domain.Member) string {
	if member.Name == "" {
		return member.NameJa + " → " + member.NameKo
	}

	return member.NameJa + " (" + member.Name + ") → " + member.NameKo
}

func buildImagePrompt(table string) string {
	return imagePromptBase + table
}

// parseTranslationTable은 형식에 맞는 줄만 이미지 프롬프트로 넘긴다. 검색 결과에서 온 설명문이 이미지 지시로 섞이지 않게 한다.
func parseTranslationTable(text string) (string, error) {
	var lines []string

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))

		original, translated, ok := strings.Cut(line, tableSeparator)
		if !ok || strings.TrimSpace(original) == "" || strings.TrimSpace(translated) == "" || strings.ContainsFunc(line, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(line) > maxTableLineRunes {
			return "", fmt.Errorf("poster translation table line exceeds %d characters", maxTableLineRunes)
		}

		lines = append(lines, strings.TrimSpace(original)+" "+tableSeparator+" "+strings.TrimSpace(translated))
	}

	if len(lines) == 0 {
		return "", errors.New("poster translation table is empty")
	}

	if len(lines) > maxTableLines {
		return "", fmt.Errorf("poster translation table exceeds %d lines", maxTableLines)
	}

	return strings.Join(lines, "\n"), nil
}
