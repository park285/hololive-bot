package youtubejs

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

// wireFixture는 youtubejs/testdata/wire-contract.json입니다. JS 검증기 테스트와 같은 본문을 읽어
// Go decode·오류 매핑과 실제 helper 왕복이 같은 계약을 따르는지 확인합니다.
type wireFixture struct {
	FailureTuples map[RPCErrorCode]struct {
		Class      RPCFailureClass `json:"class"`
		Statuses   []int           `json:"statuses"`
		RetryKinds []RPCRetryKind  `json:"retry_kinds"`
	} `json:"failure_tuples"`
	InvalidFailures []struct {
		Name   string         `json:"name"`
		Status int            `json:"status"`
		Body   jsontext.Value `json:"body"`
	} `json:"invalid_failures"`
	RoundtripFailures []struct {
		Subject string        `json:"subject"`
		Code    RPCErrorCode  `json:"code"`
		Retry   *RPCRetryHint `json:"retry"`
	} `json:"roundtrip_failures"`
	Endpoints map[string]struct {
		Minimal []jsontext.Value `json:"minimal"`
		Valid   []wireCase       `json:"valid"`
		Invalid []wireCase       `json:"invalid"`
	} `json:"endpoints"`
}

type wireCase struct {
	Name        string         `json:"name"`
	Subject     string         `json:"subject"`
	Kind        string         `json:"kind"`
	GoRejectsAt string         `json:"go_rejects_at"`
	Body        jsontext.Value `json:"body"`
}

var defaultWireRetry = RPCRetryHint{Kind: "default"}

var wireResults = map[string]func() any{
	"/v1/community":          func() any { return new(CommunityResult) },
	"/v1/content":            func() any { return new(ContentResult) },
	"/v1/channel":            func() any { return new(ChannelResult) },
	"/v1/channel_live_check": func() any { return new(ChannelLiveCheckResult) },
	"/v1/video_live_check":   func() any { return new(VideoLiveCheckResult) },
}

func loadWireFixture(t *testing.T) wireFixture {
	t.Helper()

	raw, err := os.ReadFile(absSibling(t, filepath.Join("..", "..", "..", "youtubejs", "testdata", "wire-contract.json")))
	if err != nil {
		t.Fatal(err)
	}

	var fixture wireFixture

	if err := jsonv2.Unmarshal(raw, &fixture, jsonv2.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}

	return fixture
}

func compactJSON(t *testing.T, value jsontext.Value) []byte {
	t.Helper()

	compact := value.Clone()
	if err := compact.Compact(); err != nil {
		t.Fatal(err)
	}

	return compact
}

func TestWireFailureTuplesMatchFixture(t *testing.T) {
	t.Parallel()

	fixture := loadWireFixture(t)

	if got, want := slices.Sorted(maps.Keys(helperFailureMap)), slices.Sorted(maps.Keys(fixture.FailureTuples)); !slices.Equal(got, want) {
		t.Fatalf("Go failure codes = %v, fixture = %v", got, want)
	}

	var statuses []int

	for _, tuple := range fixture.FailureTuples {
		statuses = append(statuses, tuple.Statuses...)
	}

	statuses = slices.Compact(slices.Sorted(slices.Values(statuses)))

	for code, tuple := range fixture.FailureTuples {
		for _, status := range statuses {
			failure := RPCFailure{Code: code, Class: tuple.Class, Retry: defaultWireRetry}

			if _, ok := mapHelperFailure(status, &failure); ok != slices.Contains(tuple.Statuses, status) {
				t.Errorf("mapHelperFailure(%d, %s) ok = %v", status, code, ok)
			}
		}

		for _, kind := range []RPCRetryKind{defaultWireRetry.Kind, "after", "at"} {
			if got, want := validRetryKind(code, kind), slices.Contains(tuple.RetryKinds, kind); got != want {
				t.Errorf("validRetryKind(%s, %s) = %v, want %v", code, kind, got, want)
			}
		}

		body, err := jsonv2.Marshal(RPCErrorBody{
			ProtocolVersion: ProtocolVersion,
			Error:           RPCFailure{Code: code, Class: tuple.Class, Retry: defaultWireRetry, Message: "failure"},
		})
		if err != nil {
			t.Fatal(err)
		}

		mapped := helperStatusError(tuple.Statuses[0], body)
		if collecterr.ClassOf(mapped) != contract.FailureClass(tuple.Class) {
			t.Errorf("helperStatusError(%s) class = %s, want %s", code, collecterr.ClassOf(mapped), tuple.Class)
		}
	}

	for _, invalid := range fixture.InvalidFailures {
		if err := helperStatusError(invalid.Status, compactJSON(t, invalid.Body)); collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
			t.Errorf("%s: error = %v, want protocol mismatch", invalid.Name, err)
		}
	}
}

func TestWireSuccessBodiesMatchFixture(t *testing.T) {
	t.Parallel()

	fixture := loadWireFixture(t)

	if got, want := slices.Sorted(maps.Keys(fixture.Endpoints)), slices.Sorted(maps.Keys(wireResults)); !slices.Equal(got, want) {
		t.Fatalf("fixture endpoints = %v, want %v", got, want)
	}

	for path, cases := range fixture.Endpoints {
		for _, valid := range cases.Valid {
			if err := decodeHelperSuccess(compactJSON(t, valid.Body), wireResults[path]()); err != nil {
				t.Errorf("%s %s: %v", path, valid.Name, err)
			}
		}

		for _, invalid := range cases.Invalid {
			err := decodeHelperSuccess(compactJSON(t, invalid.Body), wireResults[path]())

			switch invalid.GoRejectsAt {
			case "decode":
				if collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
					t.Errorf("%s %s: error = %v, want protocol mismatch", path, invalid.Name, err)
				}
			case "envelope", "classification":
				// 수집기 envelope·분류 단계의 거부는 youtubejscollector 테스트가 같은 본문으로 확인한다.
				if err != nil {
					t.Errorf("%s %s: decode error = %v, want envelope-stage rejection", path, invalid.Name, err)
				}
			default:
				t.Fatalf("%s %s: go_rejects_at %q is unknown", path, invalid.Name, invalid.GoRejectsAt)
			}
		}
	}
}

// Go의 사전 거부 하한이 helper 하한보다 크면 helper가 처리할 수 있는 요청을 Go가 먼저 버린다.
func TestWireMinimumSuccessBytesDoNotExceedHelperMinimum(t *testing.T) {
	t.Parallel()

	fixture := loadWireFixture(t)

	for path, cases := range fixture.Endpoints {
		helperMinimum := int64(-1)

		for _, body := range cases.Minimal {
			size := int64(len(compactJSON(t, body)))
			if helperMinimum < 0 || size < helperMinimum {
				helperMinimum = size
			}
		}

		if got := minimumSuccessResponseBytes(path); got < 1 || got > helperMinimum {
			t.Errorf("minimumSuccessResponseBytes(%s) = %d, helper minimum = %d", path, got, helperMinimum)
		}
	}
}

// 실제 server.mjs의 bootstrap·RPC 처리·응답 검증·인코딩을 거친 결과가 fixture 본문과 같아야 한다.
// 왕복 helper는 upstream fetcher만 fixture로 바꾸며 네트워크에 접속하지 않는다.
// 테스트 이름은 t.TempDir 아래 UDS 경로가 100 byte를 넘지 않게 짧게 둔다.
func TestWireRPC(t *testing.T) {
	fixture := loadWireFixture(t)

	cfg := liveHelperConfig(t, t.TempDir())

	cfg.ScriptPath = absSibling(t, filepath.Join("..", "..", "..", "youtubejs", "testdata", "wire-roundtrip-helper.mjs"))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	helper, rpc, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	defer func() {
		if err := helper.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	for path, cases := range fixture.Endpoints {
		for _, valid := range cases.Valid {
			got, err := fetchWire(ctx, rpc, path, valid.Subject, valid.Kind, 0)
			if err != nil {
				t.Errorf("%s %s: %v", path, valid.Name, err)

				continue
			}

			want := wireResults[path]()
			if err := strictDecode(compactJSON(t, valid.Body), want); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s:\n got %#v\nwant %#v", path, valid.Name, got, want)
			}
		}
	}

	for _, path := range []string{"/v1/community", "/v1/content", "/v1/channel"} {
		limit := len(compactJSON(t, fixture.Endpoints[path].Minimal[0]))

		if _, err := fetchWire(ctx, rpc, path, wireMinimalSubject, "live", limit); err != nil {
			t.Errorf("%s at helper minimum %d: %v", path, limit, err)
		}
	}

	for _, failure := range fixture.RoundtripFailures {
		_, err := fetchWire(ctx, rpc, "/v1/community", failure.Subject, "", 0)
		tuple := fixture.FailureTuples[failure.Code]

		mapped, ok := helperFailureMap[failure.Code]
		if !ok || collecterr.CodeOf(err) != mapped.mapped.code || collecterr.ClassOf(err) != contract.FailureClass(tuple.Class) {
			t.Errorf("%s: error = %v, want %s/%s", failure.Subject, err, failure.Code, tuple.Class)

			continue
		}

		assertWireRetry(t, failure.Subject, collecterr.RetryOf(err), failure.Retry)
	}
}

// wireMinimalSubject는 왕복 helper가 fixture의 최소 본문을 돌려주는 요청 subject입니다.
const wireMinimalSubject = "UCminimal"

func fetchWire(ctx context.Context, rpc *RPC, path, subject, kind string, limit int) (any, error) {
	switch path {
	case "/v1/community":
		return wireResult(rpc.FetchCommunity(ctx, CommunityRequest{ChannelID: subject, MaxResults: 10, MaxPages: 1, MaxSuccessResponseBytes: limit}))
	case "/v1/content":
		return wireResult(rpc.FetchContent(ctx, ContentRequest{ChannelID: subject, Kind: "videos", MaxResults: 10, MaxPages: 1, MaxSuccessResponseBytes: limit}))
	case "/v1/channel":
		return wireResult(rpc.FetchChannel(ctx, ChannelRequest{ChannelID: subject, Kind: kind, MaxPages: 1, MaxSuccessResponseBytes: limit}))
	case "/v1/channel_live_check":
		return wireResult(rpc.FetchChannelLiveCheck(ctx, ChannelLiveCheckRequest{ChannelID: subject, MaxSuccessResponseBytes: limit}))
	case "/v1/video_live_check":
		return wireResult(rpc.FetchVideoLiveCheck(ctx, VideoLiveCheckRequest{VideoID: subject, MaxSuccessResponseBytes: limit}))
	default:
		return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "unknown wire fixture path "+path)
	}
}

// wireResult는 decode 결과를 fixture 본문 decode와 같은 포인터 형식으로 맞춥니다.
func wireResult[T any](result T, err error) (any, error) {
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func assertWireRetry(t *testing.T, subject string, got collecterr.RetryHint, want *RPCRetryHint) {
	t.Helper()

	switch {
	case want == nil || want.Kind == defaultWireRetry.Kind:
		if got.Kind() != collecterr.RetryDefault {
			t.Errorf("%s: retry = %v, want default", subject, got.Kind())
		}
	case want.Kind == "after":
		if got.Kind() != collecterr.RetryAfter || got.After() != time.Duration(want.AfterMS)*time.Millisecond {
			t.Errorf("%s: retry = %v %s, want after %dms", subject, got.Kind(), got.After(), want.AfterMS)
		}
	case want.Kind == "at":
		at, err := time.Parse(time.RFC3339, want.At)
		if err != nil {
			t.Fatal(err)
		}

		if got.Kind() != collecterr.RetryAt || !got.At().Equal(at) {
			t.Errorf("%s: retry = %v %s, want at %s", subject, got.Kind(), got.At(), at)
		}
	default:
		t.Fatalf("%s: retry kind %q is unknown", subject, want.Kind)
	}
}
