package holo

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const projectionNullJSON = "null"

const validProjectedAlarm = `{"roomId":"9007199254740993","roomName":"한글 <방>","channelId":"UC-fixture","memberName":"멤버😀"}`

func TestProjectedAlarmsPreserveWireAndRejectMalformedInput(t *testing.T) {
	for _, record := range []string{validProjectedAlarm, `{"private":{"nested":[1,null,true]},"memberName":"멤버😀","channelId":"UC-fixture","roomName":"한글 <방>","\u0072oomId":"9007199254740993"}`} {
		var response AlarmsResponse

		require.NoError(t, jsonv2.Unmarshal([]byte(`{"alarms":[`+record+`],"private":true,"status":"ok"}`), &response))

		body, err := jsonv2.Marshal(response, jsontext.EscapeForHTML(true))
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"ok","alarms":[`+validProjectedAlarm+`]}`, string(body))
		require.Contains(t, string(body), `\u003c`)
	}

	for _, record := range []string{`[]`, `[{"roomId":"","roomName":"","channelId":"","memberName":""}]`} {
		var response AlarmsResponse

		require.NoError(t, jsonv2.Unmarshal([]byte(`{"status":"ok","alarms":`+record+`}`), &response))

		body, err := jsonv2.Marshal(response)
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"ok","alarms":`+record+`}`, string(body))
	}

	invalid := make([]string, 0, 39)

	invalid = append(invalid, projectionNullJSON, `{}`, `[]`, `{"status":"ok"}`, `{"alarms":[]}`, `{"status":null,"alarms":[]}`, `{"status":"error","alarms":[]}`, `{"status":"ok","alarms":null}`, `{"status":"ok","alarms":[null]}`, `{"status":"ok","alarms":[{}]}`, `{"status":"ok","alarms":[],"alarms":[]}`, `{"status":"ok","alarms":[]} {}`)

	for _, field := range []string{"roomId", "roomName", "channelId", "memberName"} {
		for _, value := range []string{projectionNullJSON, `0`, `true`, `{}`, `[]`} {
			record := make(map[string]any)
			require.NoError(t, jsonv2.Unmarshal([]byte(validProjectedAlarm), &record))

			var replacement any

			require.NoError(t, jsonv2.Unmarshal([]byte(value), &replacement))

			record[field] = replacement

			body, err := jsonv2.Marshal(record)
			require.NoError(t, err)

			invalid = append(invalid, `{"status":"ok","alarms":[`+string(body)+`]}`)
		}

		record := make(map[string]any)
		require.NoError(t, jsonv2.Unmarshal([]byte(validProjectedAlarm), &record))
		delete(record, field)

		body, err := jsonv2.Marshal(record)
		require.NoError(t, err)

		invalid = append(invalid, `{"status":"ok","alarms":[`+string(body)+`]}`)
	}

	invalid = append(invalid,
		`{"status":"ok","alarms":[`+strings.Replace(validProjectedAlarm, `"roomId":`, `"roomId":"duplicate","roomId":`, 1)+`]}`,
		`{"status":"ok","alarms":[`+strings.Replace(validProjectedAlarm, `"UC-fixture"`, `"bad`+string([]byte{0xff})+`"`, 1)+`]}`,
		`{"status":"ok","alarms":[`+validProjectedAlarm,
	)
	for i, body := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var response AlarmsResponse

			require.Error(t, jsonv2.Unmarshal([]byte(body), &response))
		})
	}
}
