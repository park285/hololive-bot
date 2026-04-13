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

package server

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type ProfileResponse struct {
	Status     string          `json:"status"`
	Profile    *ProfileData    `json:"profile,omitempty"`
	Translated *TranslatedData `json:"translated,omitempty"`
}

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

type DataEntry struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type SocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type TranslatedData struct {
	DisplayName string      `json:"display_name"`
	Catchphrase string      `json:"catchphrase"`
	Summary     string      `json:"summary"`
	Highlights  []string    `json:"highlights"`
	Data        []DataEntry `json:"data"`
}

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

	profile, err := h.profiles.GetByChannel(channelID)
	if err != nil {
		h.logger.Warn("Profile not found",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
		c.JSON(404, gin.H{"error": "Profile not found for channel"})

		return
	}

	_, translated, err := h.profiles.GetWithTranslation(ctx, profile.EnglishName)
	if err != nil {
		h.logger.Error("Failed to load translated profile",
			slog.String("english_name", profile.EnglishName),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to load translated profile"})

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

func convertTranslatedRows(rows []domain.TranslatedProfileDataRow) []DataEntry {
	result := make([]DataEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, DataEntry{Label: row.Label, Value: row.Value})
	}

	return result
}
