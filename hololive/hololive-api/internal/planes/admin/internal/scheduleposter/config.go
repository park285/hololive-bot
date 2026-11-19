package scheduleposter

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/genai"
)

const (
	defaultPollSeconds     = 3600
	defaultLocation        = "global"
	maxConfigBytes         = 64 << 10
	maxGlossaryTerms       = 200
	maxGlossaryRunes       = 80
	maxThumbnailVideos     = 5
	hydrogenModel          = "gemini-4-hydrogen"
	neonModel              = "gemini-4-neon"
	defaultRowsTimeout     = 180
	carbonModel            = "gemini-4-carbon"
	defaultExtractTimeout  = 900
	minRowsTimeout         = 30
	maxRowsTimeout         = 900
	maxRowsTemperature     = 2
	minChangePollSeconds   = 300
	maxChangePollSeconds   = 86400
	defaultScheduleInfoMin = 0.7
	minScheduleInfoMin     = 0.01
	maxScheduleInfoMin     = 0.99
)

var ErrDisabled = errors.New("schedule poster collection disabled")

var (
	xUserIDPattern   = regexp.MustCompile(`^[1-9]\d{0,19}$`)
	channelIDPattern = regexp.MustCompile(`^UC[a-zA-Z0-9_-]{22}$`)
	projectPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	locationPattern  = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	modelPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,79}$`)
	videoIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

type Target struct {
	XUserID                  string   `json:"x_user_id"`
	ChannelID                string   `json:"channel_id"`
	YouTubeCommunity         bool     `json:"youtube_community"`
	YouTubeThumbnailVideoIDs []string `json:"youtube_thumbnail_video_ids"`
}

type TranslationConfig struct {
	Project  string            `json:"project"`
	Location string            `json:"location"`
	Glossary map[string]string `json:"glossary"`
	Rows     RowsConfig        `json:"rows"`
}

type RowsConfig struct {
	Extract   RowStageConfig `json:"extract"`
	Translate RowStageConfig `json:"translate"`
	Match     RowStageConfig `json:"match"`
	Decide    DecideConfig   `json:"decide"`
}

type DecideConfig struct {
	ScheduleInfoMin float64 `json:"schedule_info_min"`
}

type RowStageConfig struct {
	Model          string              `json:"model"`
	ThinkingLevel  genai.ThinkingLevel `json:"thinking_level"`
	Temperature    *float32            `json:"temperature"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
}

func defaultRowsConfig() RowsConfig {
	return RowsConfig{
		Extract:   RowStageConfig{Model: carbonModel, ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: defaultExtractTimeout},
		Translate: RowStageConfig{Model: neonModel, ThinkingLevel: genai.ThinkingLevelMedium, TimeoutSeconds: defaultRowsTimeout},
		Match:     RowStageConfig{Model: hydrogenModel, ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: defaultRowsTimeout},
		Decide:    DecideConfig{ScheduleInfoMin: defaultScheduleInfoMin},
	}
}

func (c RowStageConfig) timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

func (c RowStageConfig) validate(stage string) error {
	levels := []genai.ThinkingLevel{genai.ThinkingLevelMinimal, genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh}
	if !slices.Contains(levels, c.ThinkingLevel) {
		return fmt.Errorf("schedule poster rows.%s thinking_level must be MINIMAL, LOW, MEDIUM or HIGH", stage)
	}

	if !modelPattern.MatchString(c.Model) || c.TimeoutSeconds < minRowsTimeout || c.TimeoutSeconds > maxRowsTimeout ||
		c.Temperature != nil && !(*c.Temperature >= 0 && *c.Temperature <= maxRowsTemperature) {
		return fmt.Errorf("schedule poster rows.%s requires a model, a %d-%d second timeout and a 0-%d temperature",
			stage, minRowsTimeout, maxRowsTimeout, maxRowsTemperature)
	}

	return nil
}

func validChangePoll(seconds int) bool {
	return seconds == 0 || seconds >= minChangePollSeconds && seconds <= maxChangePollSeconds
}

func (c RowsConfig) validate() error {
	if err := c.Extract.validate("extract"); err != nil {
		return err
	}

	if err := c.Translate.validate("translate"); err != nil {
		return err
	}

	if err := c.Match.validate("match"); err != nil {
		return err
	}

	if minimum := c.Decide.ScheduleInfoMin; !(minimum >= minScheduleInfoMin && minimum <= maxScheduleInfoMin) {
		return fmt.Errorf("schedule poster rows.decide.schedule_info_min must be %.2f-%.2f", minScheduleInfoMin, maxScheduleInfoMin)
	}

	return nil
}

type Config struct {
	PollSeconds       int               `json:"poll_seconds"`
	ChangePollSeconds int               `json:"change_poll_seconds"`
	Targets           []Target          `json:"targets"`
	Translation       TranslationConfig `json:"translation"`
}

func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollSeconds) * time.Second
}

func (c Config) ChangePollInterval() time.Duration {
	return time.Duration(c.ChangePollSeconds) * time.Second
}

func LoadConfig(path string) (result *Config, retErr error) {
	if path == "" {
		return nil, ErrDisabled
	}

	if !filepath.IsAbs(path) {
		return nil, errors.New("schedule poster config path must be absolute")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open schedule poster config directory: %w", err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close schedule poster config directory: %w", closeErr))
		}
	}()

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("open schedule poster config: %w", err)
	}

	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil || len(raw) > maxConfigBytes {
		return nil, errors.New("read schedule poster config failed or too large")
	}

	config := Config{
		PollSeconds: defaultPollSeconds,
		Translation: TranslationConfig{Location: defaultLocation, Rows: defaultRowsConfig()},
	}
	if err := jsonv2.Unmarshal(raw, &config, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid schedule poster config JSON")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c Config) Validate() error {
	if len(c.Targets) == 0 || len(c.Targets) > 20 || c.PollSeconds < 600 || c.PollSeconds > 86400 {
		return errors.New("schedule poster requires 1-20 targets and a 600-86400 second interval")
	}

	if !validChangePoll(c.ChangePollSeconds) {
		return fmt.Errorf("schedule poster change_poll_seconds must be 0 or %d-%d", minChangePollSeconds, maxChangePollSeconds)
	}

	if err := validateTargets(c.Targets); err != nil {
		return err
	}

	translation := c.Translation
	if !projectPattern.MatchString(translation.Project) || !locationPattern.MatchString(translation.Location) {
		return errors.New("schedule poster translation requires project and location")
	}

	if err := translation.Rows.validate(); err != nil {
		return err
	}

	if len(translation.Glossary) > maxGlossaryTerms {
		return fmt.Errorf("schedule poster glossary allows at most %d terms", maxGlossaryTerms)
	}

	for term, translated := range translation.Glossary {
		if !glossaryText(term) || !glossaryText(translated) {
			return errors.New("schedule poster glossary terms must be 1-80 characters without control characters")
		}
	}

	return nil
}

func validateTargets(targets []Target) error {
	seenUsers := make(map[string]bool, len(targets))
	seenVideos := map[string]bool{}

	for _, target := range targets {
		youtube := target.YouTubeCommunity || len(target.YouTubeThumbnailVideoIDs) > 0
		validUser := target.XUserID == "" && youtube || xUserIDPattern.MatchString(target.XUserID) && !seenUsers[target.XUserID]

		if !validUser || !channelIDPattern.MatchString(target.ChannelID) {
			return errors.New("invalid or duplicate schedule poster target")
		}

		if target.XUserID != "" {
			seenUsers[target.XUserID] = true
		}

		if len(target.YouTubeThumbnailVideoIDs) > maxThumbnailVideos {
			return fmt.Errorf("schedule poster target allows at most %d thumbnail videos", maxThumbnailVideos)
		}

		for _, videoID := range target.YouTubeThumbnailVideoIDs {
			if !videoIDPattern.MatchString(videoID) || seenVideos[videoID] {
				return errors.New("invalid or duplicate schedule poster thumbnail video ID")
			}

			seenVideos[videoID] = true
		}
	}

	return nil
}

func glossaryText(text string) bool {
	count := utf8.RuneCountInString(strings.TrimSpace(text))

	return count > 0 && count <= maxGlossaryRunes && !strings.ContainsFunc(text, unicode.IsControl)
}
