package youtubejs

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func helperStatusError(status int, payload []byte) error {
	var decoded RPCErrorBody

	if err := strictDecode(payload, &decoded); err != nil {
		return protocolMismatch(fmt.Errorf("decode youtube.js helper error response: %w", err))
	}

	if decoded.ProtocolVersion != ProtocolVersion {
		return protocolMismatch(errors.New("youtube.js helper error protocol version mismatch"))
	}

	mapped, ok := mapHelperFailure(status, &decoded.Error)
	if !ok {
		return protocolMismatch(errors.New("youtube.js helper status/body tuple mismatch"))
	}

	// helper는 조회가 없으면 필드를 생략하므로 명시적 0도 protocol 위반입니다.
	if decoded.ScheduleLookups != nil && (*decoded.ScheduleLookups < 1 || *decoded.ScheduleLookups > ChannelLiveMaxScheduleLookups) {
		return protocolMismatch(errors.New("youtube.js helper error schedule lookups are outside the protocol limit"))
	}

	base := collecterr.New(mapped.code, mapped.class, "youtube.js helper: "+boundedFailureMessage(decoded.Error.Message))

	failure := applyHelperRetry(base, decoded.Error.Retry)

	lookups := 0

	if decoded.ScheduleLookups != nil {
		lookups = *decoded.ScheduleLookups
	}

	return &scheduleLookupError{lookups: lookups, err: failure}
}

// scheduleLookupError는 검증된 helper 오류 본문의 오류와 그 본문이 보고한 예정 영상 조회 수(생략은 0)를 함께
// 전달합니다. 이 오류가 없으면 helper가 조회 수를 보고하지 못한 것입니다. 분류는 감싼 helper 오류를 그대로 따릅니다.
type scheduleLookupError struct {
	lookups int
	err     error
}

func (e *scheduleLookupError) Error() string { return e.err.Error() }
func (e *scheduleLookupError) Unwrap() error { return e.err }

type mappedFailure struct {
	code  contract.CollectionErrorCode
	class contract.FailureClass
}

var helperFailureMap = map[RPCErrorCode]struct {
	class    RPCFailureClass
	statuses map[int]struct{}
	mapped   mappedFailure
}{
	"invalid_request":           {"PROTOCOL", statusSet(400, 404), mappedFailure{contract.ErrorHelperProtocolMismatch, contract.ClassProtocol}},
	"request_too_large":         {"PROTOCOL", statusSet(413), mappedFailure{contract.ErrorHelperProtocolMismatch, contract.ClassProtocol}},
	"helper_not_ready":          {"PROTOCOL", statusSet(503), mappedFailure{contract.ErrorHelperProtocolMismatch, contract.ClassProtocol}},
	"helper_busy":               {"TRANSIENT", statusSet(503), mappedFailure{contract.ErrorHelperBusy, contract.ClassTransient}},
	"collection_canceled":       {"CANCELED", statusSet(408), mappedFailure{contract.ErrorCollectionCanceled, contract.ClassCanceled}},
	"collection_timeout":        {"TIMEOUT", statusSet(408, 504), mappedFailure{contract.ErrorCollectionTimeout, contract.ClassTimeout}},
	"cooldown":                  {"COOLDOWN", statusSet(429), mappedFailure{contract.ErrorCooldown, contract.ClassCooldown}},
	"parser_drift":              {"DATA_CONTRACT", statusSet(422), mappedFailure{contract.ErrorParserDrift, contract.ClassDataContract}},
	"configuration_error":       {"CONFIGURATION", statusSet(502), mappedFailure{contract.ErrorConfiguration, contract.ClassConfiguration}},
	"response_too_large":        {"RESOURCE_LIMIT", statusSet(422), mappedFailure{contract.ErrorResponseTooLarge, contract.ClassResourceLimit}},
	"helper_protocol_mismatch":  {"PROTOCOL", statusSet(409), mappedFailure{contract.ErrorHelperProtocolMismatch, contract.ClassProtocol}},
	"helper_internal_invariant": {"INTERNAL", statusSet(500), mappedFailure{contract.ErrorInternalInvariant, contract.ClassInternal}},
	"collection_failed":         {"TRANSIENT", statusSet(502), mappedFailure{contract.ErrorCollectionFailed, contract.ClassTransient}},
}

func statusSet(statuses ...int) map[int]struct{} {
	result := make(map[int]struct{}, len(statuses))
	for _, status := range statuses {
		result[status] = struct{}{}
	}

	return result
}

func mapHelperFailure(status int, failure *RPCFailure) (mappedFailure, bool) {
	if failure == nil {
		return mappedFailure{}, false
	}

	entry, ok := helperFailureMap[failure.Code]
	if !ok || failure.Class != entry.class {
		return mappedFailure{}, false
	}

	if _, ok := entry.statuses[status]; !ok {
		return mappedFailure{}, false
	}

	if !validRetryKind(failure.Code, failure.Retry.Kind) {
		return mappedFailure{}, false
	}

	return entry.mapped, true
}

func validRetryKind(code RPCErrorCode, kind RPCRetryKind) bool {
	switch code {
	case "helper_busy":
		return kind == "default" || kind == "after"
	case "cooldown":
		return kind == "default" || kind == "after" || kind == "at"
	default:
		return kind == "default"
	}
}

func applyHelperRetry(base error, retry RPCRetryHint) error {
	switch retry.Kind {
	case "default":
		return applyDefaultRetry(base, retry)
	case "after":
		return applyAfterRetry(base, retry)
	case "at":
		return applyAtRetry(base, retry)
	default:
		return protocolMismatch(errors.New("youtube.js helper retry kind is unknown"))
	}
}

func applyDefaultRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS != 0 || retry.At != "" {
		return protocolMismatch(errors.New("youtube.js helper default retry carries a payload"))
	}

	return base
}

func applyAfterRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS <= 0 || retry.At != "" {
		return protocolMismatch(errors.New("youtube.js helper after retry is invalid"))
	}

	hint, err := collecterr.NewRetryAfterHint(time.Duration(retry.AfterMS) * time.Millisecond)
	if err != nil {
		return protocolMismatch(err)
	}

	return collecterr.WithRetry(base, hint)
}

func applyAtRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS != 0 || retry.At == "" {
		return protocolMismatch(errors.New("youtube.js helper at retry is invalid"))
	}

	at, err := time.Parse(time.RFC3339, retry.At)
	if err != nil {
		return protocolMismatch(fmt.Errorf("youtube.js helper at retry is invalid: %w", err))
	}

	hint, err := collecterr.NewRetryAtHint(at)
	if err != nil {
		return protocolMismatch(err)
	}

	return collecterr.WithRetry(base, hint)
}

func protocolMismatch(err error) error {
	return collecterr.Wrap(contract.ErrorHelperProtocolMismatch, contract.ClassProtocol, err)
}

func boundedFailureMessage(message string) string {
	message = collecterr.SanitizeDetail(message)
	if len(message) > 512 {
		end := 512
		for end > 0 && !utf8.RuneStart(message[end]) {
			end--
		}

		message = message[:end]
	}

	if strings.TrimSpace(message) == "" {
		return "helper failure"
	}

	return message
}
