package youtubejs

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func helperStatusError(status int, payload []byte) error {
	var decoded RPCErrorBody

	if err := strictDecode(payload, &decoded); err != nil {
		return errors.Join(protocolMismatchError(fmt.Errorf("decode youtube.js helper error response: %w", err)))
	}

	if decoded.ProtocolVersion != ProtocolVersion {
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper error protocol version mismatch")))
	}

	mapped, ok := mapHelperFailure(status, &decoded.Error)
	if !ok {
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper status/body tuple mismatch")))
	}

	base := collecterr.New(mapped.code, mapped.class, "youtube.js helper: "+boundedFailureMessage(decoded.Error.Message))

	if err := applyHelperRetry(base, decoded.Error.Retry); err != nil {
		return fmt.Errorf("apply helper retry: %w", err)
	}

	return nil
}

type mappedFailure struct {
	code  collecterr.ErrorCode
	class collecterr.FailureClass
}

var helperFailureMap = map[RPCErrorCode]struct {
	class    RPCFailureClass
	statuses map[int]struct{}
	mapped   mappedFailure
}{
	"invalid_request":           {"PROTOCOL", statusSet(400, 404), mappedFailure{collecterr.HelperProtocolMismatch, collecterr.ClassProtocol}},
	"request_too_large":         {"PROTOCOL", statusSet(413), mappedFailure{collecterr.HelperProtocolMismatch, collecterr.ClassProtocol}},
	"helper_not_ready":          {"PROTOCOL", statusSet(503), mappedFailure{collecterr.HelperProtocolMismatch, collecterr.ClassProtocol}},
	"helper_busy":               {"TRANSIENT", statusSet(503), mappedFailure{collecterr.HelperBusy, collecterr.ClassTransient}},
	"collection_canceled":       {"CANCELED", statusSet(408), mappedFailure{collecterr.Canceled, collecterr.ClassCanceled}},
	"collection_timeout":        {"TIMEOUT", statusSet(408, 504), mappedFailure{collecterr.Timeout, collecterr.ClassTimeout}},
	"cooldown":                  {"COOLDOWN", statusSet(429), mappedFailure{collecterr.Cooldown, collecterr.ClassCooldown}},
	"parser_drift":              {"DATA_CONTRACT", statusSet(422), mappedFailure{collecterr.ParserDrift, collecterr.ClassDataContract}},
	"configuration_error":       {"CONFIGURATION", statusSet(502), mappedFailure{collecterr.Configuration, collecterr.ClassConfiguration}},
	"response_too_large":        {"RESOURCE_LIMIT", statusSet(422), mappedFailure{collecterr.ResponseTooLarge, collecterr.ClassResourceLimit}},
	"helper_protocol_mismatch":  {"PROTOCOL", statusSet(409), mappedFailure{collecterr.HelperProtocolMismatch, collecterr.ClassProtocol}},
	"helper_internal_invariant": {"INTERNAL", statusSet(500), mappedFailure{collecterr.Internal, collecterr.ClassInternal}},
	"collection_failed":         {"TRANSIENT", statusSet(502), mappedFailure{collecterr.Failed, collecterr.ClassTransient}},
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
		return errors.Join(applyDefaultRetryResult(base, retry))
	case "after":
		return errors.Join(applyAfterRetryResult(base, retry))
	case "at":
		return errors.Join(applyAtRetryResult(base, retry))
	default:
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper retry kind is unknown")))
	}
}

func applyDefaultRetryResult(base error, retry RPCRetryHint) error {
	if err := applyDefaultRetry(base, retry); err != nil {
		return fmt.Errorf("apply default retry: %w", err)
	}

	return nil
}

func applyAfterRetryResult(base error, retry RPCRetryHint) error {
	if err := applyAfterRetry(base, retry); err != nil {
		return fmt.Errorf("apply after retry: %w", err)
	}

	return nil
}

func applyAtRetryResult(base error, retry RPCRetryHint) error {
	if err := applyAtRetry(base, retry); err != nil {
		return fmt.Errorf("apply at retry: %w", err)
	}

	return nil
}

func applyDefaultRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS != 0 || retry.At != "" {
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper default retry carries a payload")))
	}

	return base
}

func applyAfterRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS <= 0 || retry.At != "" {
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper after retry is invalid")))
	}

	hint, err := collecterr.NewRetryAfterHint(time.Duration(retry.AfterMS) * time.Millisecond)
	if err != nil {
		return errors.Join(protocolMismatchError(err))
	}

	if err := collecterr.WithRetry(base, hint); err != nil {
		return fmt.Errorf("with retry: %w", err)
	}

	return nil
}

func applyAtRetry(base error, retry RPCRetryHint) error {
	if retry.AfterMS != 0 || retry.At == "" {
		return errors.Join(protocolMismatchError(errors.New("youtube.js helper at retry is invalid")))
	}

	at, err := time.Parse(time.RFC3339, retry.At)
	if err != nil {
		return errors.Join(protocolMismatchError(fmt.Errorf("youtube.js helper at retry is invalid: %w", err)))
	}

	hint, err := collecterr.NewRetryAtHint(at)
	if err != nil {
		return errors.Join(protocolMismatchError(err))
	}

	if err := collecterr.WithRetry(base, hint); err != nil {
		return fmt.Errorf("with retry: %w", err)
	}

	return nil
}

func protocolMismatch(err error) error {
	return collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, err)
}

func protocolMismatchError(err error) error {
	return fmt.Errorf("protocol mismatch: %w", protocolMismatch(err))
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
