// Package scheduleposter는 X·유튜브의 멤버 주간 일정 포스터를 후보로 수집하고, 관리자가 확정한 포스터를 한국어 이미지로 번역한다.
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
	defaultModel           = "gemini-nano-banana-2.1"
	defaultTextModel       = "gemini-4-argon"
	defaultTranscribeModel = "gemini-4-hydrogen"
	maxConfigBytes         = 64 << 10
	maxGlossaryTerms       = 200
	maxGlossaryRunes       = 80
	maxThumbnailVideos     = 5
	defaultRowsModel       = "gemini-4-hydrogen"
	defaultRowsTimeout     = 180
	defaultExtractModel    = "gemini-4-carbon"
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

// Target은 X 계정(XUserID), 유튜브 커뮤니티(YouTubeCommunity), 영상 썸네일(YouTubeThumbnailVideoIDs) 가운데 하나 이상을 후보 출처로 둔다.
type Target struct {
	XUserID          string `json:"x_user_id"`
	ChannelID        string `json:"channel_id"`
	YouTubeCommunity bool   `json:"youtube_community"`
	// YouTubeThumbnailVideoIDs는 멤버가 주간 일정을 썸네일로 바꿔 다는 영상(프리챗 등)이다. 그림이 바뀔 때마다 후보가 된다.
	YouTubeThumbnailVideoIDs []string `json:"youtube_thumbnail_video_ids"`
}

// TranslationConfig의 인증은 GOOGLE_APPLICATION_CREDENTIALS(ADC)로 따로 공급한다. 파일에는 비밀을 넣지 않는다.
type TranslationConfig struct {
	Project  string `json:"project"`
	Location string `json:"location"`
	Model    string `json:"model"`
	// TextModel은 포스터 글자를 읽어 번역표를 만드는 모델이고, Model은 그 표대로 이미지를 다시 그린다.
	TextModel string `json:"text_model"`
	// TranscribeModel은 원본과 번역본의 글자를 대조용으로 판독한다.
	TranscribeModel string `json:"transcribe_model"`
	// Glossary는 멤버 이름(members 표에서 채움) 밖의 용어 표기다.
	Glossary map[string]string `json:"glossary"`
	Rows     RowsConfig        `json:"rows"`
}

// RowsConfig는 일정표 행 추출과 번역 호출의 단계별 모델 파라미터다. Vertex 클라이언트는 TranslationConfig와 함께 쓴다.
type RowsConfig struct {
	Extract   RowStageConfig `json:"extract"`
	Translate RowStageConfig `json:"translate"`
	Match     RowStageConfig `json:"match"`
	Decide    DecideConfig   `json:"decide"`
}

// DecideConfig의 ScheduleInfoMin을 바꿔도 이미 판별한 글은 다시 판별하지 않는다.
type DecideConfig struct {
	ScheduleInfoMin float64 `json:"schedule_info_min"`
}

// RowStageConfig의 Temperature가 nil이면 요청에 넣지 않아 모델 기본값을 쓴다. TimeoutSeconds는 Vertex 요청 하나의 상한이다.
type RowStageConfig struct {
	Model          string              `json:"model"`
	ThinkingLevel  genai.ThinkingLevel `json:"thinking_level"`
	Temperature    *float32            `json:"temperature"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
}

func defaultRowsConfig() RowsConfig {
	return RowsConfig{
		Extract:   RowStageConfig{Model: defaultExtractModel, ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: defaultExtractTimeout},
		Translate: RowStageConfig{Model: defaultRowsModel, ThinkingLevel: genai.ThinkingLevelMedium, TimeoutSeconds: defaultRowsTimeout},
		Match:     RowStageConfig{Model: defaultRowsModel, ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: defaultRowsTimeout},
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

// Config의 ChangePollSeconds가 0이면 변경 감지를 끈다. 키를 넣어야 새 외부 호출 루프가 켜진다.
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

// LoadConfig는 SCHEDULE_POSTER_CONFIG_FILE 경로가 비어 있으면 ErrDisabled를 돌려준다.
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
		Translation: TranslationConfig{
			Location: defaultLocation, Model: defaultModel, TextModel: defaultTextModel, TranscribeModel: defaultTranscribeModel,
			Rows: defaultRowsConfig(),
		},
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
	if !projectPattern.MatchString(translation.Project) || !locationPattern.MatchString(translation.Location) ||
		!modelPattern.MatchString(translation.Model) || !modelPattern.MatchString(translation.TextModel) ||
		!modelPattern.MatchString(translation.TranscribeModel) {
		return errors.New("schedule poster translation requires project, location, model, text_model and transcribe_model")
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

// glossaryText는 줄바꿈 같은 제어 문자를 막아 한 항목이 프롬프트의 한 줄을 넘지 않게 한다.
func glossaryText(text string) bool {
	count := utf8.RuneCountInString(strings.TrimSpace(text))

	return count > 0 && count <= maxGlossaryRunes && !strings.ContainsFunc(text, unicode.IsControl)
}
