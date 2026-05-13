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

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

func main() {
	ctx := context.Background()

	runtime, err := app.BuildFetchProfilesRuntime(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize runtime: %v\n", err)
		os.Exit(1)
	}
	defer runtime.Close()

	logger := runtime.Logger

	talents, err := domain.LoadTalents()
	if err != nil {
		logger.Error("failed to load official talents list", slog.Any("error", err))
		os.Exit(1)
	}

	httpClient := runtime.HTTPClient

	profiles := make(map[string]*domain.TalentProfile, len(talents.Talents))
	for idx, talent := range talents.Talents {
		if talent == nil || stringutil.TrimSpace(talent.English) == "" {
			continue
		}

		slug := talent.Slug()
		english := stringutil.TrimSpace(talent.English)

		profileURL := fmt.Sprintf("%s/%s/", constants.OfficialProfileConfig.BaseURL, slug)
		logger.Info("Fetching profile", slog.Int("index", idx+1), slog.String("slug", slug), slog.String("url", profileURL))

		profile, err := fetchProfile(ctx, httpClient, profileURL, english, slug)
		if err != nil {
			logger.Error("failed to fetch profile", slog.String("slug", slug), slog.Any("error", err))
			continue
		}

		profiles[slug] = profile

		time.Sleep(constants.OfficialProfileConfig.DelayBetween)
	}

	if len(profiles) == 0 {
		logger.Error("no profiles fetched")
		os.Exit(1)
	}

	if err := writeProfiles(profiles); err != nil {
		logger.Error("failed to write profiles", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("Profile fetch completed",
		slog.Int("count", len(profiles)),
		slog.String("output", constants.OfficialProfileConfig.OutputFile),
	)
}

func fetchProfile(ctx context.Context, client *http.Client, url, englishName, slug string) (*domain.TalentProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", constants.OfficialProfileConfig.UserAgent)
	req.Header.Set("Accept-Language", constants.OfficialProfileConfig.AcceptLanguage)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	profile := &domain.TalentProfile{
		Slug:        slug,
		OfficialURL: url,
	}

	rightBox := doc.Find(".right_box").First()
	if rightBox.Length() == 0 {
		return nil, errors.New("profile container not found")
	}

	header := rightBox.Find("h1").First()
	if header.Length() > 0 {
		japanese := stringutil.TrimSpace(header.Clone().Children().Remove().Text())
		english := stringutil.TrimSpace(header.Find("span").First().Text())

		if english != "" {
			profile.EnglishName = english
		} else {
			profile.EnglishName = englishName
		}

		if japanese != "" {
			profile.JapaneseName = japanese
		}
	} else {
		profile.EnglishName = englishName
	}

	catchText := normalizeText(rightBox.Find("p.catch").First().Text())

	profile.Catchphrase = catchText

	descText := normalizeText(rightBox.Find("p.txt").First().Text())

	profile.Description = descText

	profile.SocialLinks = extractSocialLinks(rightBox.Find(".t_sns a"))
	profile.DataEntries = extractDataEntries(doc.Find(".talent_data .table_box dl"))

	return profile, nil
}

func extractSocialLinks(selection *goquery.Selection) []domain.TalentSocialLink {
	links := make([]domain.TalentSocialLink, 0, selection.Length())
	selection.Each(func(_ int, sel *goquery.Selection) {
		label := stringutil.TrimSpace(sel.Text())
		href, _ := sel.Attr("href")

		url := stringutil.TrimSpace(href)
		if label == "" || url == "" {
			return
		}

		links = append(links, domain.TalentSocialLink{Label: label, URL: url})
	})

	return links
}

func extractDataEntries(selection *goquery.Selection) []domain.TalentProfileEntry {
	entries := make([]domain.TalentProfileEntry, 0, selection.Length())
	selection.Each(func(_ int, sel *goquery.Selection) {
		label := stringutil.TrimSpace(sel.Find("dt").First().Text())

		value := normalizeText(sel.Find("dd").First().Text())
		if label == "" || value == "" {
			return
		}

		entries = append(entries, domain.TalentProfileEntry{Label: label, Value: value})
	})

	return entries
}

func normalizeText(input string) string {
	input = strings.ReplaceAll(input, "\u00a0", " ")

	lines := strings.Split(input, "\n")

	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = stringutil.TrimSpace(line)
		if line == "" {
			continue
		}

		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

func writeProfiles(profiles map[string]*domain.TalentProfile) error {
	outputFile := constants.OfficialProfileConfig.OutputFile

	if err := os.MkdirAll(filepath.Dir(outputFile), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}

	tmpFile := outputFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, outputFile); err != nil {
		return fmt.Errorf("failed to rename output file: %w", err)
	}

	splitDir := filepath.Join(filepath.Dir(outputFile), "official_profiles_raw")
	if err := os.MkdirAll(splitDir, 0o750); err != nil {
		return fmt.Errorf("failed to create split directory: %w", err)
	}

	for slug, profile := range profiles {
		bytes, err := json.MarshalIndent(profile, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal profile %s: %w", slug, err)
		}

		tmp := filepath.Join(splitDir, slug+".json.tmp")
		target := filepath.Join(splitDir, slug+".json")

		if err := os.WriteFile(tmp, bytes, 0o600); err != nil {
			return fmt.Errorf("failed to write profile %s: %w", slug, err)
		}

		if err := os.Rename(tmp, target); err != nil {
			return fmt.Errorf("failed to finalize profile %s: %w", slug, err)
		}
	}

	return nil
}
