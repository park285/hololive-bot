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
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	runtime, err := app.BuildFetchProfilesRuntime(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize runtime: %w", err)
	}
	defer runtime.Close()

	logger := runtime.Logger
	talents, err := domain.LoadTalents()
	if err != nil {
		return fmt.Errorf("failed to load official talents list: %w", err)
	}

	profiles := fetchProfiles(ctx, runtime.HTTPClient, logger, talents.Talents)
	if len(profiles) == 0 {
		return errors.New("no profiles fetched")
	}

	if err := writeProfiles(profiles); err != nil {
		return fmt.Errorf("failed to write profiles: %w", err)
	}

	logger.Info("Profile fetch completed",
		slog.Int("count", len(profiles)),
		slog.String("output", config.DefaultOfficialProfileConfig().OutputFile),
	)
	return nil
}

func fetchProfiles(
	ctx context.Context,
	client *http.Client,
	logger *slog.Logger,
	talents []*domain.OfficialTalent,
) map[string]*domain.TalentProfile {
	profiles := make(map[string]*domain.TalentProfile, len(talents))
	for idx, talent := range talents {
		profile := fetchTalentProfile(ctx, client, logger, idx, talent)
		if profile == nil {
			continue
		}
		profiles[profile.Slug] = profile
		time.Sleep(config.DefaultOfficialProfileConfig().DelayBetween)
	}
	return profiles
}

func fetchTalentProfile(
	ctx context.Context,
	client *http.Client,
	logger *slog.Logger,
	idx int,
	talent *domain.OfficialTalent,
) *domain.TalentProfile {
	if talent == nil || stringutil.TrimSpace(talent.English) == "" {
		return nil
	}

	slug := talent.Slug()
	english := stringutil.TrimSpace(talent.English)
	profileURL := fmt.Sprintf("%s/%s/", config.DefaultOfficialProfileConfig().BaseURL, slug)
	logger.Info("Fetching profile", slog.Int("index", idx+1), slog.String("slug", slug), slog.String("url", profileURL))

	profile, err := fetchProfile(ctx, client, profileURL, english, slug)
	if err != nil {
		logger.Error("failed to fetch profile", slog.String("slug", slug), slog.Any("error", err))
		return nil
	}
	return profile
}

func fetchProfile(ctx context.Context, client *http.Client, url, englishName, slug string) (*domain.TalentProfile, error) {
	resp, err := fetchProfileResponse(ctx, client, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	return buildTalentProfile(doc, url, englishName, slug)
}

func fetchProfileResponse(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", config.DefaultOfficialProfileConfig().UserAgent)
	req.Header.Set("Accept-Language", config.DefaultOfficialProfileConfig().AcceptLanguage)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return resp, nil
}

func buildTalentProfile(doc *goquery.Document, url, englishName, slug string) (*domain.TalentProfile, error) {
	profile := &domain.TalentProfile{
		Slug:        slug,
		OfficialURL: url,
	}

	rightBox := doc.Find(".right_box").First()
	if rightBox.Length() == 0 {
		return nil, errors.New("profile container not found")
	}

	applyProfileHeader(profile, rightBox.Find("h1").First(), englishName)
	profile.Catchphrase = normalizeText(rightBox.Find("p.catch").First().Text())
	profile.Description = normalizeText(rightBox.Find("p.txt").First().Text())
	profile.SocialLinks = extractSocialLinks(rightBox.Find(".t_sns a"))
	profile.DataEntries = extractDataEntries(doc.Find(".talent_data .table_box dl"))

	return profile, nil
}

func applyProfileHeader(profile *domain.TalentProfile, header *goquery.Selection, fallbackEnglish string) {
	profile.EnglishName = fallbackEnglish
	if header.Length() == 0 {
		return
	}

	headerClone := header.Clone()
	headerClone.Children().Remove()
	japanese := stringutil.TrimSpace(headerClone.Text())
	english := stringutil.TrimSpace(header.Find("span").First().Text())
	if english != "" {
		profile.EnglishName = english
	}
	if japanese != "" {
		profile.JapaneseName = japanese
	}
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
	outputFile := config.DefaultOfficialProfileConfig().OutputFile
	if err := writeJSONFile(outputFile, profiles); err != nil {
		return err
	}
	splitDir := filepath.Join(filepath.Dir(outputFile), "official_profiles_raw")

	return writeSplitProfiles(splitDir, profiles)
}

func writeSplitProfiles(splitDir string, profiles map[string]*domain.TalentProfile) error {
	if err := os.MkdirAll(splitDir, 0o750); err != nil {
		return fmt.Errorf("failed to create split directory: %w", err)
	}

	for slug, profile := range profiles {
		target := filepath.Join(splitDir, slug+".json")
		if err := writeJSONFile(target, profile); err != nil {
			return fmt.Errorf("failed to write profile %s: %w", slug, err)
		}
	}

	return nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to rename output file: %w", err)
	}
	return nil
}
