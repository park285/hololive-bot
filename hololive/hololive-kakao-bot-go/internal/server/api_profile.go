package server

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// ProfileResponse: 프로필 API 응답 구조체
// 원본 프로필과 번역된 프로필 정보를 함께 반환함
type ProfileResponse struct {
	Status     string          `json:"status"`
	Profile    *ProfileData    `json:"profile,omitempty"`
	Translated *TranslatedData `json:"translated,omitempty"`
}

// ProfileData: 원본 프로필 데이터 (영문 기반)
type ProfileData struct {
	Slug         string       `json:"slug"`
	EnglishName  string       `json:"english_name"`
	JapaneseName string       `json:"japanese_name"`
	Catchphrase  string       `json:"catchphrase"`
	Description  string       `json:"description"`
	DataEntries  []DataEntry  `json:"data_entries"`
	SocialLinks  []SocialLink `json:"social_links"`
	OfficialURL  string       `json:"official_url"`
}

// DataEntry: 프로필 데이터 항목 (레이블-값 쌍)
type DataEntry struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SocialLink: 소셜 미디어 링크
type SocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// TranslatedData: 번역된 프로필 데이터 (한국어)
type TranslatedData struct {
	DisplayName string      `json:"display_name"`
	Catchphrase string      `json:"catchphrase"`
	Summary     string      `json:"summary"`
	Highlights  []string    `json:"highlights"`
	Data        []DataEntry `json:"data"`
}

// GetProfile: 채널 ID로 멤버 프로필을 조회합니다.
// Query params:
//   - channelId: YouTube 채널 ID (필수)
func (h *ProfileAPIHandler) GetProfile(c *gin.Context) {
	channelID := c.Query("channelId")
	if channelID == "" {
		c.JSON(400, gin.H{"error": "channelId is required"})
		return
	}

	if h.profiles == nil {
		h.logger.Error("ProfileService is not initialized")
		c.JSON(500, gin.H{"error": "Profile service unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	// 채널 ID로 프로필 조회
	profile, err := h.profiles.GetByChannel(channelID)
	if err != nil {
		h.logger.Warn("Profile not found",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
		c.JSON(404, gin.H{"error": "Profile not found for channel"})
		return
	}

	// 번역 정보 조회
	_, translated, err := h.profiles.GetWithTranslation(ctx, profile.EnglishName)
	if err != nil {
		h.logger.Error("Failed to load translated profile",
			slog.String("english_name", profile.EnglishName),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to load translated profile"})
		return
	}

	// 응답 구조체 변환
	resp := ProfileResponse{
		Status:  "ok",
		Profile: convertToProfileData(profile),
	}

	if translated != nil {
		resp.Translated = &TranslatedData{
			DisplayName: translated.DisplayName,
			Catchphrase: translated.Catchphrase,
			Summary:     translated.Summary,
			Highlights:  translated.Highlights,
			Data:        convertTranslatedRows(translated.Data),
		}
	}

	c.JSON(200, resp)
}

// GetProfileByName: 영문 이름으로 멤버 프로필을 조회합니다.
// Query params:
//   - name: 영문 이름 (필수)
func (h *ProfileAPIHandler) GetProfileByName(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(400, gin.H{"error": "name is required"})
		return
	}

	if h.profiles == nil {
		h.logger.Error("ProfileService is not initialized")
		c.JSON(500, gin.H{"error": "Profile service unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	profile, translated, err := h.profiles.GetWithTranslation(ctx, name)
	if err != nil {
		h.logger.Warn("Profile not found",
			slog.String("name", name),
			slog.Any("error", err),
		)
		c.JSON(404, gin.H{"error": "Profile not found"})
		return
	}

	resp := ProfileResponse{
		Status:  "ok",
		Profile: convertToProfileData(profile),
	}

	if translated != nil {
		resp.Translated = &TranslatedData{
			DisplayName: translated.DisplayName,
			Catchphrase: translated.Catchphrase,
			Summary:     translated.Summary,
			Highlights:  translated.Highlights,
			Data:        convertTranslatedRows(translated.Data),
		}
	}

	c.JSON(200, resp)
}

// convertToProfileData: domain.TalentProfile을 API 응답 구조체로 변환
func convertToProfileData(p *domain.TalentProfile) *ProfileData {
	if p == nil {
		return nil
	}

	entries := make([]DataEntry, 0, len(p.DataEntries))
	for _, e := range p.DataEntries {
		entries = append(entries, DataEntry{Label: e.Label, Value: e.Value})
	}

	links := make([]SocialLink, 0, len(p.SocialLinks))
	for _, l := range p.SocialLinks {
		links = append(links, SocialLink{Label: l.Label, URL: l.URL})
	}

	return &ProfileData{
		Slug:         p.Slug,
		EnglishName:  p.EnglishName,
		JapaneseName: p.JapaneseName,
		Catchphrase:  p.Catchphrase,
		Description:  p.Description,
		DataEntries:  entries,
		SocialLinks:  links,
		OfficialURL:  p.OfficialURL,
	}
}

// convertTranslatedRows: domain.TranslatedProfileDataRow 슬라이스를 API 응답 형식으로 변환
func convertTranslatedRows(rows []domain.TranslatedProfileDataRow) []DataEntry {
	result := make([]DataEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, DataEntry{Label: row.Label, Value: row.Value})
	}
	return result
}
