package scheduleposter

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func writeChangeConfig(t *testing.T, top, rows string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "schedule-poster.json")
	body := `{"targets":[{"x_user_id":"2054825907916054528","channel_id":"UCROQtXcp2loQEmvpe5rhJzQ"}]` + top +
		`,"translation":{"project":"example-project","rows":{` + rows + `}}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestLoadConfigLeavesChangeDetectionOffByDefault(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(writeChangeConfig(t, "", ""))
	require.NoError(t, err)
	require.Zero(t, config.ChangePollSeconds)
	require.Zero(t, config.ChangePollInterval())
	require.Equal(t, RowStageConfig{Model: hydrogen, ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: 180}, config.Translation.Rows.Match)
	require.Equal(t, DecideConfig{ScheduleInfoMin: 0.7}, config.Translation.Rows.Decide)

	config, err = LoadConfig(writeChangeConfig(t, `,"change_poll_seconds":0`, ""))
	require.NoError(t, err)
	require.Zero(t, config.ChangePollInterval())
}

func TestLoadConfigReadsChangeSettings(t *testing.T) {
	t.Parallel()

	config, err := LoadConfig(writeChangeConfig(t, `,"change_poll_seconds":600`,
		`"match":{"thinking_level":"MEDIUM","temperature":0.2},"decide":{"schedule_info_min":0.8}`))
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, config.ChangePollInterval())
	require.Equal(t, RowStageConfig{Model: hydrogen, ThinkingLevel: genai.ThinkingLevelMedium, Temperature: new(float32(0.2)), TimeoutSeconds: 180},
		config.Translation.Rows.Match, "넣지 않은 키는 기본값을 지킨다")
	require.Equal(t, DecideConfig{ScheduleInfoMin: 0.8}, config.Translation.Rows.Decide)
	require.Equal(t, defaultRowsConfig().Extract, config.Translation.Rows.Extract)

	config, err = LoadConfig(writeChangeConfig(t, `,"change_poll_seconds":300`, `"match":{"model":"gemini-4-carbon","timeout_seconds":900},"decide":{}`))
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, config.ChangePollInterval())
	require.Equal(t, RowStageConfig{Model: "gemini-4-carbon", ThinkingLevel: genai.ThinkingLevelLow, TimeoutSeconds: 900}, config.Translation.Rows.Match)
	require.Equal(t, DecideConfig{ScheduleInfoMin: 0.7}, config.Translation.Rows.Decide)

	config, err = LoadConfig(writeChangeConfig(t, `,"change_poll_seconds":86400`, `"decide":{"schedule_info_min":0.01}`))
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, config.ChangePollInterval())
	require.InDelta(t, 0.01, config.Translation.Rows.Decide.ScheduleInfoMin, 0)
}

func TestLoadConfigRejectsInvalidChangeSettings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ top, rows string }{
		{`,"change_poll_seconds":299`, ""},
		{`,"change_poll_seconds":1`, ""},
		{`,"change_poll_seconds":-1`, ""},
		{`,"change_poll_seconds":86401`, ""},
		{`,"change_poll_seconds":"600"`, ""},
		{"", `"decide":{"schedule_info_min":0}`},
		{"", `"decide":{"schedule_info_min":0.005}`},
		{"", `"decide":{"schedule_info_min":0.995}`},
		{"", `"decide":{"schedule_info_min":1}`},
		{"", `"decide":{"schedule_info_min":0.7,"kind_min":0.5}`},
		{"", `"match":{"thinking_level":"low"}`},
		{"", `"match":{"timeout_seconds":29}`},
		{"", `"match":{"model":"Gemini 4"}`},
		{"", `"match":{"top_p":0.9}`},
		{"", `"judge":{"model":"gemini-4-hydrogen"}`},
		{`,"change_seconds":600`, ""},
	} {
		_, err := LoadConfig(writeChangeConfig(t, tc.top, tc.rows))
		require.Error(t, err, "%s %s", tc.top, tc.rows)
	}
}
