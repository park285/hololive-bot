package youtubejscollector

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type channelWireFixture struct {
	Valid []struct {
		Name    string         `json:"name"`
		Subject string         `json:"subject"`
		Kind    string         `json:"kind"`
		Body    jsontext.Value `json:"body"`
	} `json:"valid"`
	Invalid []struct {
		Name        string         `json:"name"`
		Subject     string         `json:"subject"`
		GoRejectsAt string         `json:"go_rejects_at"`
		Body        jsontext.Value `json:"body"`
	} `json:"invalid"`
}

// loadChannelWireFixture는 youtubejs/testdata/wire-contract.json의 /v1/channel 본문을 읽습니다.
// JS 검증기 테스트와 같은 본문을 실제 RPC decode와 live snapshot envelope 생성에 통과시킵니다.
func loadChannelWireFixture(t *testing.T) channelWireFixture {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "youtubejs", "testdata", "wire-contract.json"))
	if err != nil {
		t.Fatal(err)
	}

	var fixture struct {
		Endpoints map[string]channelWireFixture `json:"endpoints"`
	}

	if err := jsonv2.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	return fixture.Endpoints["/v1/channel"]
}

func TestChannelLiveRunnerPublishesWireFixture(t *testing.T) {
	t.Parallel()

	for _, valid := range loadChannelWireFixture(t).Valid {
		if valid.Kind != channelKindLive {
			continue
		}

		var body struct {
			LiveSessions []jsontext.Value `json:"live_sessions"`
		}

		if err := jsonv2.Unmarshal(valid.Body, &body); err != nil {
			t.Fatal(err)
		}

		output, err := NewChannelLiveRunner(fixtureRPC(t, valid.Body)).Collect(t.Context(), channelLiveFixtureInput(t, valid.Subject))
		if err != nil {
			t.Fatalf("%s: %v", valid.Name, err)
		}

		observations := output.Output().Observations()
		if len(observations) != 1 {
			t.Fatalf("%s: observations = %d", valid.Name, len(observations))
		}

		var payload contract.LiveSnapshotV1

		if err := jsonv2.Unmarshal(observations[0].Payload, &payload); err != nil {
			t.Fatal(err)
		}

		if len(payload.Sessions) != len(body.LiveSessions) || payload.Query == nil || !payload.Query.AccessRestricted {
			t.Fatalf("%s: payload = %#v", valid.Name, payload)
		}
	}
}

// JS 검증기가 거부하는 본문 중 Go가 envelope 단계에서 거부하는 사례는 출력 없이 parser drift여야 합니다.
func TestChannelLiveRunnerRejectsWireFixtureContradictions(t *testing.T) {
	t.Parallel()

	for _, invalid := range loadChannelWireFixture(t).Invalid {
		if invalid.GoRejectsAt != "envelope" {
			continue
		}

		output, err := NewChannelLiveRunner(fixtureRPC(t, invalid.Body)).Collect(t.Context(), channelLiveFixtureInput(t, invalid.Subject))
		if collecterr.CodeOf(err) != contract.ErrorParserDrift || !output.IsZero() {
			t.Fatalf("%s: error = %v output = %#v, want parser drift without output", invalid.Name, err, output)
		}
	}
}

func channelLiveFixtureInput(t *testing.T, subject string) *collection.RunInput {
	t.Helper()

	return youtubeInputWithLiveGeneration(t, subject, "youtubejs_channel_live",
		contract.LiveSnapshotQueryContractGeneration, contract.KindLiveSnapshot)
}

// fixtureRPC는 helper 응답 본문 하나를 돌려주는 HTTP 서버에 실제 youtubejs.RPC를 연결합니다.
func fixtureRPC(t *testing.T, body jsontext.Value) *youtubejs.RPC {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	return youtubejs.NewRPC(server.Client(), server.URL, nil)
}
