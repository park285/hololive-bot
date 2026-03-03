package member

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	translationLocale         = "ko"
	cacheKeyProfileTranslated = "hololive:profile:translated:%s:%s"
)

// ProfileService: 탤런트 상세 프로필 정보를 관리하는 서비스
// 로컬 파일에서 데이터를 로드하고, 번역 정보를 관리하며 캐싱을 지원한다.
type ProfileService struct {
	cache         cache.Client
	logger        *slog.Logger
	membersData   domain.MemberDataProvider
	profiles      map[string]*domain.TalentProfile // slug -> profile
	translations  map[string]*domain.Translated
	englishToSlug map[string]string
	channelToSlug map[string]string
}

// NewProfileService: 프로필 데이터와 번역 데이터를 로드하여 서비스 인스턴스를 초기화합니다.
// 검색 최적화를 위한 인덱싱 작업도 수행한다.
func NewProfileService(cacheSvc cache.Client, membersData domain.MemberDataProvider, logger *slog.Logger) (*ProfileService, error) {
	if membersData == nil {
		return nil, fmt.Errorf("members data is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	profiles, err := domain.LoadProfiles()
	if err != nil {
		return nil, fmt.Errorf("failed to load official profiles dataset: %w", err)
	}

	preTranslated, err := domain.LoadTranslated()
	if err != nil {
		return nil, fmt.Errorf("failed to load translated profiles dataset: %w", err)
	}

	service := &ProfileService{
		cache:         cacheSvc,
		logger:        logger,
		membersData:   membersData,
		profiles:      profiles,
		translations:  preTranslated,
		englishToSlug: make(map[string]string, len(profiles)),
		channelToSlug: make(map[string]string, len(membersData.GetAllMembers())),
	}

	for slug, profile := range profiles {
		if profile == nil {
			continue
		}
		key := stringutil.NormalizeKey(profile.EnglishName)
		if key != "" {
			service.englishToSlug[key] = slug
		}
	}

	for _, member := range membersData.GetAllMembers() {
		if member == nil {
			continue
		}
		if slug, ok := service.slugFor(member.Name); ok {
			service.channelToSlug[stringutil.Normalize(member.ChannelID)] = slug
			continue
		}

		key := stringutil.NormalizeKey(member.Name)
		if key != "" {
			service.englishToSlug[key] = stringutil.Slugify(member.Name)
		}
	}

	logger.Info("ProfileService initialized",
		slog.Int("profiles", len(service.profiles)),
		slog.Int("translated_profiles", len(service.translations)),
		slog.Int("index_english", len(service.englishToSlug)),
		slog.Int("index_channel", len(service.channelToSlug)),
	)

	return service, nil
}

// GetWithTranslation: 영문 이름으로 프로필을 조회하고, 번역된 정보가 있다면 함께 반환합니다.
func (s *ProfileService) GetWithTranslation(ctx context.Context, englishName string) (*domain.TalentProfile, *domain.Translated, error) {
	if stringutil.TrimSpace(englishName) == "" {
		return nil, nil, fmt.Errorf("member name is required")
	}

	profile, err := s.GetByEnglish(englishName)
	if err != nil {
		return nil, nil, err
	}

	translated, err := s.getTranslated(ctx, profile)
	if err != nil {
		return nil, nil, err
	}

	return profile, translated, nil
}

// GetByEnglish: 영문 이름(정규화됨)으로 원본 프로필 정보를 조회합니다.
func (s *ProfileService) GetByEnglish(englishName string) (*domain.TalentProfile, error) {
	if profile, ok := s.byEnglish(englishName); ok {
		return profile, nil
	}
	return nil, fmt.Errorf("official profile not found for member '%s'", englishName)
}

// GetByChannel: 채널 ID로 원본 프로필 정보를 조회합니다.
func (s *ProfileService) GetByChannel(channelID string) (*domain.TalentProfile, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	slug, ok := s.channelToSlug[stringutil.Normalize(channelID)]
	if !ok {
		return nil, fmt.Errorf("no official profile for channel ID '%s'", channelID)
	}
	profile, ok := s.profiles[slug]
	if !ok || profile == nil {
		return nil, fmt.Errorf("no profile data for slug '%s'", slug)
	}
	return profile, nil
}

func (s *ProfileService) byEnglish(englishName string) (*domain.TalentProfile, bool) {
	slug, ok := s.slugFor(englishName)
	if !ok {
		return nil, false
	}
	profile, ok := s.profiles[slug]
	if !ok || profile == nil {
		return nil, false
	}
	return profile, true
}

func (s *ProfileService) slugFor(name string) (string, bool) {
	key := stringutil.NormalizeKey(name)
	if key == "" {
		return "", false
	}
	slug, ok := s.englishToSlug[key]
	return slug, ok
}

func (s *ProfileService) getTranslated(ctx context.Context, raw *domain.TalentProfile) (*domain.Translated, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw profile is nil")
	}

	cacheKey := fmt.Sprintf(cacheKeyProfileTranslated, translationLocale, raw.Slug)

	if s.cache != nil {
		var cached domain.Translated
		if err := s.cache.Get(ctx, cacheKey, &cached); err == nil && cached.DisplayName != "" {
			return &cached, nil
		}
	}

	if translated := s.translations[raw.Slug]; translated != nil {
		cloned := cloneTranslatedProfile(translated)
		if s.cache != nil && cloned != nil {
			if err := s.cache.Set(ctx, cacheKey, cloned, 0); err != nil {
				s.logger.Warn("Failed to cache translated profile",
					slog.String("slug", raw.Slug),
					slog.Any("error", err),
				)
			}
		}
		return cloned, nil
	}

	// Fallback: raw profile에서 단순 번역 생성 (AI 미사용)
	fallback := &domain.Translated{
		DisplayName: raw.EnglishName,
		Catchphrase: raw.Catchphrase,
		Summary:     raw.Description,
		Highlights:  []string{},
		Data:        convertToTranslatedRows(raw.DataEntries),
	}
	if s.cache != nil {
		if err := s.cache.Set(ctx, cacheKey, fallback, 0); err != nil {
			s.logger.Warn("Failed to cache fallback translated profile",
				slog.String("slug", raw.Slug),
				slog.Any("error", err),
			)
		}
	}
	return fallback, nil
}

func convertToTranslatedRows(entries []domain.TalentProfileEntry) []domain.TranslatedProfileDataRow {
	if len(entries) == 0 {
		return []domain.TranslatedProfileDataRow{}
	}
	rows := make([]domain.TranslatedProfileDataRow, 0, len(entries))
	for _, e := range entries {
		label := stringutil.TrimSpace(e.Label)
		value := stringutil.TrimSpace(e.Value)
		if label == "" || value == "" {
			continue
		}
		rows = append(rows, domain.TranslatedProfileDataRow{Label: label, Value: value})
	}
	return rows
}

// PreloadTranslations: 모든 번역 데이터를 캐시에 미리 적재하여 조회 성능을 높인다.
func (s *ProfileService) PreloadTranslations(ctx context.Context) {
	if s == nil || s.cache == nil || len(s.translations) == 0 {
		return
	}

	written := 0
	for slug, profile := range s.translations {
		if profile == nil {
			continue
		}
		if err := s.cache.Set(ctx, fmt.Sprintf(cacheKeyProfileTranslated, translationLocale, slug), profile, 0); err != nil {
			s.logger.Warn("Failed to preload translated profile",
				slog.String("slug", slug),
				slog.Any("error", err),
			)
			continue
		}
		written++
	}

	if written > 0 {
		s.logger.Info("Preloaded translated profiles",
			slog.Int("count", written))
	}
}

func cloneTranslatedProfile(src *domain.Translated) *domain.Translated {
	if src == nil {
		return nil
	}

	clone := *src
	if len(src.Highlights) > 0 {
		clone.Highlights = append([]string(nil), src.Highlights...)
	}
	if len(src.Data) > 0 {
		clone.Data = make([]domain.TranslatedProfileDataRow, len(src.Data))
		copy(clone.Data, src.Data)
	}
	return new(clone)
}
