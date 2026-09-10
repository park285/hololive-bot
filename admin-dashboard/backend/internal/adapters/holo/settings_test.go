package holo

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func mutationFixtures(t *testing.T) map[string]any {
	t.Helper()

	name, err := memberFixtureClient(t, `{"status":"ok"}`).SetUserName(t.Context(), UserNameRequest{UserID: "9007199254740993", UserName: "한글"})
	require.NoError(t, err)

	deleted, err := memberFixtureClient(t, `{"status":"ok","removed":false}`).DeleteAlarm(t.Context(), DeleteAlarmRequest{RoomID: "9007199254740993", ChannelID: "UC"})
	require.NoError(t, err)

	enabled := false
	acl, err := memberFixtureClient(t, `{"status":"ok","enabled":false,"mode":"whitelist","message":"ignored"}`).SetACL(t.Context(), ACLRequest{Enabled: &enabled})
	require.NoError(t, err)

	minutes := 15
	settings, err := memberFixtureClient(t, `{"status":"ok","settings":{"alarmAdvanceMinutes":15},"runtime":{"alarm_applied":true,"alarm_requested_advance_minutes":15,"alarm_target_minutes":[15,0],"config_publish_alarm_advance_minutes":false,"config_publish_alarm_advance_minutes_error":"synthetic-private-error"}}`).UpdateSettings(t.Context(), SettingsUpdateRequest{AlarmAdvanceMinutes: &minutes})
	require.NoError(t, err)

	return map[string]any{"StatusOnlyResponse": []StatusOnlyResponse{name}, "DeleteAlarmResponse": []DeleteAlarmResponse{deleted}, "SetAclResponse": []ACLResponse{acl}, "SettingsUpdateResponse": []SettingsUpdateResponse{settings}}
}

func TestSettingsPreserveSaveApplyAndPublishFailure(t *testing.T) {
	minutes := 15
	client := memberFixtureClient(t, `{"status":"ok","settings":{"alarmAdvanceMinutes":15,"private":"synthetic-private"},"runtime":{"alarm_applied":true,"config_publish_alarm_advance_minutes":false,"config_publish_alarm_advance_minutes_error":"synthetic-private-error"}}`)
	response, err := client.UpdateSettings(t.Context(), SettingsUpdateRequest{AlarmAdvanceMinutes: &minutes})
	require.NoError(t, err)
	require.NotNil(t, response.Settings.AlarmAdvanceMinutes)
	require.Equal(t, 15, *response.Settings.AlarmAdvanceMinutes)
	require.NotNil(t, response.Runtime.AlarmApplied)
	require.True(t, *response.Runtime.AlarmApplied)
	require.NotNil(t, response.Runtime.ConfigPublishAlarmAdvanceMinutes)
	require.False(t, *response.Runtime.ConfigPublishAlarmAdvanceMinutes)

	data, err := jsonv2.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(data), "synthetic-private")
}

func TestSettingsDoNotInventMissingRuntimeResults(t *testing.T) {
	minutes := 15
	response, err := memberFixtureClient(t, `{"status":"ok","settings":{"alarmAdvanceMinutes":15},"runtime":{}}`).UpdateSettings(t.Context(), SettingsUpdateRequest{AlarmAdvanceMinutes: &minutes})
	require.NoError(t, err)
	require.Nil(t, response.Runtime.AlarmApplied)
	require.Nil(t, response.Runtime.ConfigPublishAlarmAdvanceMinutes)

	for _, runtime := range []string{`null`, `{"alarm_applied":null}`, `{"alarm_applied":"true"}`, `{"config_publish_alarm_advance_minutes":null}`, `{"alarm_target_minutes":[null]}`, `{"config_publish_alarm_advance_minutes":true,"config_publish_alarm_advance_minutes_error":"failed"}`} {
		_, err := memberFixtureClient(t, `{"status":"ok","settings":{"alarmAdvanceMinutes":15},"runtime":`+runtime+`}`).UpdateSettings(t.Context(), SettingsUpdateRequest{AlarmAdvanceMinutes: &minutes})
		require.Error(t, err)
	}
}
