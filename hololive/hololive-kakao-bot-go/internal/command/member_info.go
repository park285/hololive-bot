package command

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// MemberInfoCommand: 홀로라이브 멤버 프로필 조회 명령어를 처리하는 커맨드입니다.
type MemberInfoCommand struct {
	BaseCommand
}

// NewMemberInfoCommand: MemberInfoCommand 인스턴스를 생성합니다.
func NewMemberInfoCommand(deps *Dependencies) *MemberInfoCommand {
	return &MemberInfoCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 커맨드 이름을 반환합니다.
func (c *MemberInfoCommand) Name() string {
	return string(domain.CommandMemberInfo)
}

// Description: 커맨드 설명을 반환합니다.
func (c *MemberInfoCommand) Description() string {
	return "홀로라이브 멤버 공식 프로필"
}

// Execute: 멤버 정보 커맨드를 실행합니다.
// 쿼리가 없으면 멤버 디렉터리를, 있으면 개별 프로필을 표시합니다.
func (c *MemberInfoCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	rawQuery := getStringParam(params, "query")
	englishCandidate := getStringParam(params, "member")
	channelID := getStringParam(params, "channel_id")

	if stringutil.TrimSpace(rawQuery) == "" &&
		stringutil.TrimSpace(englishCandidate) == "" &&
		stringutil.TrimSpace(channelID) == "" {
		return c.renderMemberDirectory(ctx, cmdCtx)
	}

	member := c.resolveMember(ctx, channelID, englishCandidate, rawQuery)
	if member == nil {
		target := englishCandidate
		if target == "" {
			target = rawQuery
		}
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(target))
	}

	rawProfile, translated, err := c.Deps().OfficialProfiles.GetWithTranslation(ctx, member.Name)
	if err != nil {
		c.log().Error("Failed to load member profile",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, fmt.Sprintf(adapter.ErrMemberProfileLoadFailed, member.Name))
	}

	message := c.Deps().Formatter.FormatTalentProfile(rawProfile, translated)
	if message == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, fmt.Sprintf(adapter.ErrMemberProfileBuildFailed, member.Name))
	}

	if member.IsGraduated {
		message = adapter.MsgGraduatedMemberWarning + message
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *MemberInfoCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().MembersData == nil ||
		c.Deps().Formatter == nil || c.Deps().OfficialProfiles == nil {
		return fmt.Errorf("member info command services not configured")
	}

	return nil
}

func (c *MemberInfoCommand) resolveMember(ctx context.Context, channelID, englishName, query string) *domain.Member {
	provider := c.Deps().MembersData.WithContext(ctx)

	if channelID != "" {
		if member := provider.FindMemberByChannelID(channelID); member != nil {
			return member
		}
	}

	if englishName != "" {
		if member := provider.FindMemberByName(englishName); member != nil {
			return member
		}
	}

	trimmed := stringutil.TrimSpace(query)
	if trimmed == "" {
		return nil
	}

	channel, err := c.Deps().Matcher.FindBestMatch(ctx, trimmed)
	if err != nil {
		c.log().Warn("Member match failed",
			slog.String("query", trimmed),
			slog.Any("error", err),
		)
		return nil
	}
	if channel == nil {
		return nil
	}

	return provider.FindMemberByChannelID(channel.ID)
}

func (c *MemberInfoCommand) log() *slog.Logger {
	if c.Deps() != nil && c.Deps().Logger != nil {
		return c.Deps().Logger
	}
	return slog.Default()
}

func getStringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	val, ok := params[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return stringutil.TrimSpace(v)
	default:
		return stringutil.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func (c *MemberInfoCommand) renderMemberDirectory(ctx context.Context, cmdCtx *domain.CommandContext) error {
	if err := c.validateDirectoryDependencies(); err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberInfoDisplayFailed)
	}

	provider := c.Deps().MembersData.WithContext(ctx)
	activeMembers := c.filterActiveMembers(provider.GetAllMembers())
	if len(activeMembers) == 0 {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrNoMemberInfoFound)
	}

	groupEntries := c.buildGroupEntries(ctx, activeMembers)
	if len(groupEntries) == 0 {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrNoMemberInfoFound)
	}

	ordered := c.sortGroupsByPreference(groupEntries)
	message := c.Deps().Formatter.MemberDirectory(ctx, ordered, len(activeMembers))
	if stringutil.TrimSpace(message) == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrCannotDisplayMemberInfo)
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

// 디렉토리 렌더링에 필요한 의존성 검증
func (c *MemberInfoCommand) validateDirectoryDependencies() error {
	if c.Deps() == nil ||
		c.Deps().MembersData == nil ||
		c.Deps().OfficialProfiles == nil ||
		c.Deps().Formatter == nil ||
		c.Deps().SendMessage == nil ||
		c.Deps().SendError == nil {
		return fmt.Errorf("missing dependencies")
	}
	return nil
}

// 활성 멤버만 필터링
func (c *MemberInfoCommand) filterActiveMembers(members []*domain.Member) []*domain.Member {
	activeMembers := make([]*domain.Member, 0, len(members))
	for _, member := range members {
		if member != nil && !member.IsGraduated {
			activeMembers = append(activeMembers, member)
		}
	}
	return activeMembers
}

// 멤버를 그룹별로 분류
func (c *MemberInfoCommand) buildGroupEntries(ctx context.Context, members []*domain.Member) map[string]map[string]adapter.MemberDirectoryEntry {
	groupEntries := make(map[string]map[string]adapter.MemberDirectoryEntry)

	for _, member := range members {
		if member == nil {
			continue
		}

		groups := c.memberGroups(ctx, member)
		if len(groups) == 0 {
			groups = []string{defaultMemberDirectoryGroup}
		}

		entry := adapter.MemberDirectoryEntry{
			PrimaryName:   primaryMemberName(member),
			SecondaryName: member.Name,
		}

		for _, group := range groups {
			if groupEntries[group] == nil {
				groupEntries[group] = make(map[string]adapter.MemberDirectoryEntry)
			}
			groupEntries[group][member.Name] = entry
		}
	}

	return groupEntries
}

// 그룹을 선호 순서대로 정렬
func (c *MemberInfoCommand) sortGroupsByPreference(groupEntries map[string]map[string]adapter.MemberDirectoryEntry) []adapter.MemberDirectoryGroup {
	ordered := make([]adapter.MemberDirectoryGroup, 0, len(groupEntries))
	used := make(map[string]bool)

	for _, groupName := range memberDirectoryPreferredOrder {
		if bucket, ok := groupEntries[groupName]; ok {
			ordered = append(ordered, buildMemberDirectoryGroup(groupName, bucket))
			used[groupName] = true
		}
	}

	remaining := make([]string, 0, len(groupEntries))
	for name := range groupEntries {
		if !used[name] {
			remaining = append(remaining, name)
		}
	}
	slices.Sort(remaining)

	for _, name := range remaining {
		ordered = append(ordered, buildMemberDirectoryGroup(name, groupEntries[name]))
	}

	return ordered
}

func buildMemberDirectoryGroup(groupName string, entries map[string]adapter.MemberDirectoryEntry) adapter.MemberDirectoryGroup {
	list := make([]adapter.MemberDirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry)
	}
	slices.SortStableFunc(list, func(a, b adapter.MemberDirectoryEntry) int {
		if a.PrimaryName < b.PrimaryName {
			return -1
		}
		if a.PrimaryName > b.PrimaryName {
			return 1
		}
		return 0
	})
	return adapter.MemberDirectoryGroup{
		GroupName: groupName,
		Members:   list,
	}
}

func (c *MemberInfoCommand) memberGroups(ctx context.Context, member *domain.Member) []string {
	if member == nil {
		return nil
	}

	profile, translated, err := c.Deps().OfficialProfiles.GetWithTranslation(ctx, member.Name)
	if err != nil {
		c.log().Debug("Failed to load profile for directory",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)
		return nil
	}

	rawValues := extractUnitValues(profile, translated)
	if len(rawValues) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(rawValues))
	seen := make(map[string]bool)

	for _, raw := range rawValues {
		for _, token := range splitGroupTokens(raw) {
			name := normalizeMemberGroup(token)
			if name == "" {
				continue
			}
			if !seen[name] {
				normalized = append(normalized, name)
				seen[name] = true
			}
		}
	}

	return normalized
}

func extractUnitValues(profile *domain.TalentProfile, translated *domain.Translated) []string {
	values := make([]string, 0, 2)

	if translated != nil {
		for _, row := range translated.Data {
			if strings.Contains(row.Label, "유닛") && stringutil.TrimSpace(row.Value) != "" {
				values = append(values, row.Value)
				break
			}
		}
	}

	if len(values) == 0 && profile != nil {
		for _, entry := range profile.DataEntries {
			if strings.Contains(entry.Label, "ユニット") || strings.Contains(entry.Label, "Unit") {
				if stringutil.TrimSpace(entry.Value) != "" {
					values = append(values, entry.Value)
				}
				break
			}
		}
	}

	return values
}

func splitGroupTokens(raw string) []string {
	clean := strings.ReplaceAll(raw, "／", "/")
	clean = strings.ReplaceAll(clean, "、", "/")
	clean = strings.ReplaceAll(clean, "・", "/")

	tokens := strings.Split(clean, "/")
	if len(tokens) == 0 {
		return []string{raw}
	}

	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = stringutil.TrimSpace(token)
		if token != "" {
			result = append(result, token)
		}
	}
	if len(result) == 0 {
		return []string{raw}
	}
	return result
}

func normalizeMemberGroup(name string) string {
	trimmed := stringutil.TrimSpace(name)
	if trimmed == "" {
		return defaultMemberDirectoryGroup
	}

	if idx := strings.IndexAny(trimmed, "（("); idx != -1 {
		trimmed = stringutil.TrimSpace(trimmed[:idx])
	}

	if mapped, ok := memberDirectoryGroupAliases[trimmed]; ok {
		return mapped
	}

	if strings.HasPrefix(trimmed, "ホロライブEnglish -") {
		suffix := strings.Trim(trimmed[len("ホロライブEnglish -"):], "-")
		if suffix != "" {
			return suffix
		}
	}

	if after, ok := strings.CutPrefix(trimmed, "hololive English"); ok {
		suffix := stringutil.TrimSpace(after)
		suffix = strings.Trim(suffix, "-")
		if suffix != "" {
			return suffix
		}
	}

	return trimmed
}

func primaryMemberName(member *domain.Member) string {
	if member == nil {
		return ""
	}
	primary := strings.Trim(stringutil.TrimSpace(member.NameKo), ",")
	if primary != "" {
		return primary
	}
	return member.Name
}

const defaultMemberDirectoryGroup = "기타"

var memberDirectoryPreferredOrder = []string{
	"Advent",
	"FLOW GLOW",
	"Justice",
	"Myth",
	"Promise",
	"ReGLOSS",
	"비밀결사 holoX",
	"홀로라이브 0기생",
	"홀로라이브 1기생",
	"홀로라이브 2기생",
	"홀로라이브 3기생",
	"홀로라이브 4기생",
	"홀로라이브 5기생",
	"홀로라이브 게이머즈",
	"홀로라이브 인도네시아",
}

var memberDirectoryGroupAliases = map[string]string{
	"秘密結社holoX":                       "비밀결사 holoX",
	"ホロライブ0期生":                        "홀로라이브 0기생",
	"ホロライブ1期生":                        "홀로라이브 1기생",
	"ホロライブ2期生":                        "홀로라이브 2기생",
	"ホロライブ3期生":                        "홀로라이브 3기생",
	"ホロライブ4期生":                        "홀로라이브 4기생",
	"ホロライブ5期生":                        "홀로라이브 5기생",
	"ホロライブゲーマーズ":                      "홀로라이브 게이머즈",
	"ホロライブインドネシア":                     "홀로라이브 인도네시아",
	"ホロライブインドネシア（hololive Indonesia）": "홀로라이브 인도네시아",
	"Myth（神話）":                        "Myth",
	"Promise（約束）":                     "Promise",
	"ホロライブEnglish -Myth-":             "Myth",
	"ホロライブEnglish -Promise-":          "Promise",
	"hololive English Myth":           "Myth",
	"hololive English Promise":        "Promise",
}
