package holo

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectedHTTPJSONMatchesEncoderAndEscapesHTML(t *testing.T) {
	for _, text := range []string{"", "한글😀", "<>&", `\<>&`, `\u003c`, `"</script><script>`, "line\u2028next"} {
		name, err := jsonv2.Marshal(text)
		require.NoError(t, err)

		input := `{"status":"ok","alarms":[{"roomId":"9007199254740993","roomName":` + string(name) + `,"channelId":"UC","memberName":` + string(name) + `}]}`

		var alarms AlarmsResponse

		require.NoError(t, jsonv2.Unmarshal([]byte(input), &alarms))
		assertProjectedWriter(t, &alarms)

		member := strings.TrimSuffix(memberFixture, "}") + `,"nameKo":` + string(name) + `,"aliases":{"ko":[` + string(name) + `],"ja":[]}}`

		var members MembersResponse

		require.NoError(t, jsonv2.Unmarshal([]byte(`{"status":"ok","members":[`+member+`]}`), &members))
		assertProjectedWriter(t, &members)
	}
}

func assertProjectedWriter(t *testing.T, value interface{ WriteJSONTo(io.Writer) error }) {
	t.Helper()

	var actual bytes.Buffer

	require.NoError(t, value.WriteJSONTo(&actual))

	want, err := jsonv2.Marshal(value, jsontext.EscapeForHTML(true))
	require.NoError(t, err)
	require.JSONEq(t, string(want), actual.String())
	require.NotContains(t, actual.String(), "<")
	require.NotContains(t, actual.String(), ">")
	require.NotContains(t, actual.String(), "&")
}

type projectionFailedWriter struct{ err error }

func (w projectionFailedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestProjectedWriterRejectsUninitializedValuesAndPropagatesIO(t *testing.T) {
	for _, response := range []*AlarmsResponse{nil, {}} {
		var output bytes.Buffer

		require.Error(t, response.WriteJSONTo(&output))
		require.Empty(t, output.Bytes())
	}

	var response AlarmsResponse

	require.NoError(t, jsonv2.Unmarshal([]byte(`{"status":"ok","alarms":[`+validProjectedAlarm+`]}`), &response))

	cause := errors.New("fixture write failed")
	require.ErrorIs(t, response.WriteJSONTo(projectionFailedWriter{cause}), cause)
}

func TestProjectedRowsRequireStrictDecoderOptions(t *testing.T) {
	for _, options := range []jsonv2.Options{jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)} {
		var response AlarmsResponse

		require.Error(t, jsonv2.Unmarshal([]byte(`{"status":"ok","alarms":[]}`), &response, options))
	}
}
