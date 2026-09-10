package holo

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

func TestMemberOptionalNamesMatchOwnedDTOSerialization(t *testing.T) {
	for _, value := range []*string{nil, new(""), new("한글 <>&😀")} {
		encodedName, err := jsonv2.Marshal(value)
		require.NoError(t, err)

		input := strings.TrimSuffix(memberFixture, "}") + `,"nameJa":` + string(encodedName) + `,"nameKo":` + string(encodedName) + `,"aliases":null}`
		response, err := memberFixtureClient(t, `{"status":"ok","members":[`+input+`]}`).GetMembers(t.Context())
		require.NoError(t, err)

		actual, err := jsonv2.Marshal(response)
		require.NoError(t, err)

		expected := struct {
			Status  string            `json:"status"`
			Members []contract.Member `json:"members"`
		}{Status: "ok", Members: []contract.Member{{ID: "9007199254740993", ChannelID: "UC-fixture", Name: "한글😀", NameJA: value, NameKO: value, Aliases: contract.Aliases{KO: []string{}, JA: []string{}}}}}
		golden, err := jsonv2.Marshal(expected)
		require.NoError(t, err)
		require.JSONEq(t, string(golden), string(actual))
	}
}

func TestMemberProjectionRejectsInvalidRequiredValuesAndDuplicateNames(t *testing.T) {
	for _, id := range []string{`0`, `-1`, `9223372036854775808`, `"1"`, projectionNullJSON, `1.5`, `true`} {
		body := strings.Replace(memberFixture, "9007199254740993", id, 1)
		_, err := memberFixtureClient(t, `{"status":"ok","members":[`+body+`]}`).GetMembers(t.Context())
		require.Error(t, err)
	}

	for _, body := range []string{
		strings.Replace(memberFixture, `"channelId":"UC-fixture"`, `"channelId":null`, 1),
		strings.Replace(memberFixture, `"name":"한글😀"`, `"name":null`, 1),
		strings.Replace(memberFixture, `"isGraduated":false`, `"isGraduated":null`, 1),
		strings.Replace(memberFixture, `"isGraduated":false`, `"isGraduated":"false"`, 1),
		strings.Replace(memberFixture, `"name":`, `"\u006eame":"duplicate","name":`, 1),
		strings.Replace(memberFixture, `"name":"한글😀"`, `"name":"bad`+string([]byte{0xff})+`"`, 1),
	} {
		_, err := memberFixtureClient(t, `{"status":"ok","members":[`+body+`]}`).GetMembers(t.Context())
		require.Error(t, err)
	}
}

func TestProjectedRowsOwnBytesAfterReaderAndScratchReuse(t *testing.T) {
	body := []byte(`{"status":"ok","alarms":[` + validProjectedAlarm + `]}`)

	var first AlarmsResponse

	require.NoError(t, jsonv2.Unmarshal(body, &first))
	clear(body)

	var second AlarmsResponse

	require.NoError(t, jsonv2.Unmarshal([]byte(`{"status":"ok","alarms":[]}`), &second))

	actual, err := jsonv2.Marshal(first)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"ok","alarms":[`+validProjectedAlarm+`]}`, string(actual))
}

func TestProjectedRowsRemainDistinctAcrossStorageBlocks(t *testing.T) {
	type row struct {
		RoomID     string `json:"roomId"`
		RoomName   string `json:"roomName"`
		ChannelID  string `json:"channelId"`
		MemberName string `json:"memberName"`
	}

	input := struct {
		Status string `json:"status"`
		Alarms []row  `json:"alarms"`
	}{Status: "ok", Alarms: make([]row, 300)}
	for index := range input.Alarms {
		input.Alarms[index] = row{RoomID: fmt.Sprint(9007199254740993 + index), RoomName: strings.Repeat("한글", index%31+1), ChannelID: fmt.Sprintf("UC-%d", index), MemberName: strings.Repeat("이름", 64)}
	}

	body, err := jsonv2.Marshal(input)
	require.NoError(t, err)

	want := string(body)

	var response AlarmsResponse

	require.NoError(t, jsonv2.Unmarshal(body, &response))
	clear(body)

	actual, err := jsonv2.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, want, string(actual))
}
