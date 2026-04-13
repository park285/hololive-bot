// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

type ProfileService struct {
	cache         cache.Client
	logger        *slog.Logger
	membersData   domain.MemberDataProvider
	profiles      map[string]*domain.TalentProfile // slug -> profile
	translations  map[string]*domain.Translated
	englishToSlug map[string]string
	channelToSlug map[string]string
}

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

	members, err := domain.LoadAllMembers(membersData)
	if err != nil {
		return nil, fmt.Errorf("load members data: %w", err)
	}

	service := &ProfileService{
		cache:         cacheSvc,
		logger:        logger,
		membersData:   membersData,
		profiles:      profiles,
		translations:  preTranslated,
		englishToSlug: make(map[string]string, len(profiles)),
		channelToSlug: make(map[string]string, len(members)),
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

	for _, member := range members {
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

func (s *ProfileService) GetByEnglish(englishName string) (*domain.TalentProfile, error) {
	if profile, ok := s.byEnglish(englishName); ok {
		return profile, nil
	}
	return nil, fmt.Errorf("official profile not found for member '%s'", englishName)
}

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
