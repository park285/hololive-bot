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
	tableSeparator     = "⇒"
	transcriptPrefix   = "읽음:"
	maxTableLines      = 120
	maxTableLineRunes  = 200
	maxTranscriptLines = 200
	textPromptBase     = `이 VTuber 주간 일정 포스터에 보이는 글자를 줄마다 빠짐없이 읽어, 포스터에 다시 그릴 한국어 줄과 짝지은 번역표를 만들어라.
- 한 줄에 하나씩 "원문 줄 ⇒ 한국어 줄" 형식으로만 출력하고, 설명·머리말·맺음말을 쓰지 않는다.
- 포스터의 위에서 아래, 왼쪽에서 오른쪽 순서로 적는다. 같은 문구가 여러 곳에 있으면 나올 때마다 따로 적는다.
- 원문 줄에는 포스터의 한 줄(또는 한 칸)에 있는 글자를 시각·날짜·숫자·로마자(영어 제목, 로마자 이름, @계정명)·기호까지 모두 그대로 옮긴다. 일본어가 없는 줄도 적는다. 너무 작아 읽을 수 없는 장식 글자만 뺀다.
- 한국어 줄은 원문 줄의 일본어만 한국어로 옮기고, 숫자·로마자·기호는 원문 그대로 같은 순서로 둔다. 영어 요일만 있는 줄(MON~SUN)은 월·화·수·목·금·토·일로 쓴다. 한국어 줄에 일본어 글자를 남기지 않는다.
- 사람 이름은 아래 이름 대응표의 한국어 표기를 그대로 쓴다. 장식 글꼴이나 가나 섞임 때문에 원문이 대응표와 한두 글자 달라 보여도 가장 가까운 항목을 쓴다. 이름의 한자를 뜻으로 풀어 옮기지 않는다.
- 용어 대응표에 있는 말은 그 표기를 쓴다.
- 대응표에 없는 고유 명사(게임, 기획, 유닛, 방송 코너 이름)는 Google 검색으로 한국 팬들이 쓰는 표기를 확인해 쓰고, 확인되지 않으면 일본어 발음대로 한글로 적는다.
- 방송 제목은 한국 VTuber 팬들이 쓰는 말투와 용어(콜라보, 멤버십 한정, 잡담 등)로 자연스럽게 옮긴다. 말장난은 뜻과 말맛을 함께 살리고, 원문에 없는 내용을 더하거나 빼지 않는다.`
	imagePromptBase = `이 VTuber 주간 일정 포스터의 글자를 아래 번역표의 한국어 줄로 바꾼 같은 포스터 이미지를 만들어라.
- 번역표의 줄마다 원문 줄이 있던 자리에 한국어 줄을 빠짐없이 그린다. 한국어 줄의 글자를 고치거나 빼거나 덧붙이지 않고, 시각·숫자·로마자·기호도 한 글자도 바꾸지 않는다.
- 번역표에 없는 글자는 원본 그대로 둔다.
- 배치, 캐릭터 그림, 배경, 색, 장식, 아이콘, 이미지 비율은 그대로 둔다. 한글은 원문 글자와 같은 크기·굵기·색·외곽선과 비슷한 글꼴 분위기로 또렷하게 그리고, 칸을 넘치면 글자 크기만 줄인다.
- 원문에 없는 글자나 장식을 더하지 않는다.

번역표:
`
	transcribePrompt = `이 이미지에 보이는 글자를 모두 한 줄씩 보이는 그대로 옮겨 적어라.
- 각 줄 앞에 "읽음: "을 붙이고, 그 밖의 말은 쓰지 않는다.
- 글자를 고치거나 번역하거나 안 보이는 글자를 짐작해 채우지 않는다. 한국어·일본어·숫자·로마자·기호를 보이는 그대로 적는다.
- 위에서 아래, 왼쪽에서 오른쪽 순서로 적고, 같은 글자가 여러 곳에 있으면 나올 때마다 적는다.
- 너무 작아 읽을 수 없는 장식 글자만 뺀다.`
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

func buildImagePrompt(table translationTable, problems []string) string {
	return withProblems(imagePromptBase+table.String(), problems)
}

// parseTranslationTable은 형식에 맞는 줄만 이미지 프롬프트로 넘긴다. 검색 결과에서 온 설명문이 이미지 지시로 섞이지 않게 한다.
func parseTranslationTable(text string) (translationTable, error) {
	var table translationTable

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))

		original, translated, ok := strings.Cut(line, tableSeparator)

		original, translated = strings.TrimSpace(original), strings.TrimSpace(translated)

		if !ok || original == "" || translated == "" || strings.ContainsFunc(line, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(line) > maxTableLineRunes {
			return nil, fmt.Errorf("poster translation table line exceeds %d characters", maxTableLineRunes)
		}

		table = append(table, tableRow{original: original, korean: translated})
	}

	if len(table) == 0 {
		return nil, errors.New("poster translation table is empty")
	}

	if len(table) > maxTableLines {
		return nil, fmt.Errorf("poster translation table exceeds %d lines", maxTableLines)
	}

	return table, nil
}

// parseTranscript는 "읽음:"으로 시작하는 줄만 판독으로 받는다. 머리말 같은 설명문이 대조에 섞이지 않게 한다.
func parseTranscript(text string) ([]string, error) {
	var lines []string

	for line := range strings.Lines(text) {
		read, ok := strings.CutPrefix(strings.TrimPrefix(strings.TrimSpace(line), "- "), transcriptPrefix)
		if read = strings.TrimSpace(read); !ok || read == "" || strings.ContainsFunc(read, unicode.IsControl) {
			continue
		}

		if utf8.RuneCountInString(read) > maxTableLineRunes {
			return nil, fmt.Errorf("poster transcript line exceeds %d characters", maxTableLineRunes)
		}

		lines = append(lines, read)
	}

	if len(lines) == 0 {
		return nil, errors.New("poster transcript is empty")
	}

	if len(lines) > maxTranscriptLines {
		return nil, fmt.Errorf("poster transcript exceeds %d lines", maxTranscriptLines)
	}

	return lines, nil
}
