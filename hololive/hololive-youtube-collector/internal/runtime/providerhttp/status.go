package providerhttp

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type providerStatusClassification struct {
	code  collecterr.ErrorCode
	class collecterr.FailureClass
}

var fixedProviderStatuses = map[int]providerStatusClassification{
	http.StatusMovedPermanently:        {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusFound:                   {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusSeeOther:                {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusTemporaryRedirect:       {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusPermanentRedirect:       {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusBadRequest:              {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusNotFound:                {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusMethodNotAllowed:        {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusConflict:                {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusGone:                    {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusUnsupportedMediaType:    {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusUnprocessableEntity:     {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusUnauthorized:            {code: collecterr.Configuration, class: collecterr.ClassConfiguration},
	http.StatusForbidden:               {code: collecterr.Configuration, class: collecterr.ClassConfiguration},
	http.StatusRequestTimeout:          {code: collecterr.Failed, class: collecterr.ClassTransient},
	http.StatusTooEarly:                {code: collecterr.Failed, class: collecterr.ClassTransient},
	http.StatusInternalServerError:     {code: collecterr.Failed, class: collecterr.ClassTransient},
	http.StatusBadGateway:              {code: collecterr.Failed, class: collecterr.ClassTransient},
	http.StatusGatewayTimeout:          {code: collecterr.Failed, class: collecterr.ClassTransient},
	http.StatusNotImplemented:          {code: collecterr.Failed, class: collecterr.ClassProtocol},
	http.StatusHTTPVersionNotSupported: {code: collecterr.Failed, class: collecterr.ClassProtocol},
}

func mapProviderStatus(provider contract.Provider, status int, retryAfter, diagnostic string) error {
	message := statusMessage(provider, status, diagnostic)
	if status == http.StatusOK {
		return nil
	}

	if status == http.StatusTooManyRequests {
		if err := withRetryHint(collecterr.New(collecterr.Cooldown, collecterr.ClassCooldown, message), retryAfter); err != nil {
			return fmt.Errorf("with retry hint: %w", err)
		}

		return nil
	}

	if status == http.StatusServiceUnavailable {
		if err := mapServiceUnavailable(message, retryAfter); err != nil {
			return fmt.Errorf("map service unavailable: %w", err)
		}

		return nil
	}

	if classification, ok := fixedProviderStatuses[status]; ok {
		return collecterr.New(classification.code, classification.class, message)
	}

	return fmt.Errorf("map unknown provider status: %w", mapUnknownProviderStatus(status, message))
}

func mapUnknownProviderStatus(status int, message string) error {
	if status >= 500 && status < 600 {
		return collecterr.New(collecterr.Failed, collecterr.ClassTransient, message)
	}

	return collecterr.New(collecterr.Failed, collecterr.ClassProtocol, message)
}

func mapServiceUnavailable(message, retryAfter string) error {
	hint := collecterr.ParseRetryAfter(retryAfter, time.Time{})
	if hint.Kind() == collecterr.RetryDefault {
		return collecterr.New(collecterr.Failed, collecterr.ClassTransient, message)
	}

	if err := collecterr.WithRetry(collecterr.New(collecterr.Cooldown, collecterr.ClassCooldown, message), hint); err != nil {
		return fmt.Errorf("with retry: %w", err)
	}

	return nil
}

func withRetryHint(err error, retryAfter string) error {
	if withErr := collecterr.WithRetry(err, collecterr.ParseRetryAfter(retryAfter, time.Time{})); withErr != nil {
		return fmt.Errorf("with retry: %w", withErr)
	}

	return nil
}

func statusMessage(provider contract.Provider, status int, diagnostic string) string {
	message := fmt.Sprintf("%s status %d", provider, status)

	diagnostic = collecterr.SanitizeDetail(diagnostic)

	if diagnostic == "" {
		return message
	}

	return message + ": " + diagnostic
}

func MapRequestError(action string, err error, secrets ...string) error {
	if err == nil {
		return nil
	}

	cause := fmt.Errorf("%s: %s", action, redactRequestText(err.Error(), secrets...))
	normalized := collecterr.FromContext(err)

	if collecterr.CodeOf(normalized) == collecterr.Timeout {
		return collecterr.Wrap(collecterr.Timeout, collecterr.ClassTimeout, cause)
	}

	if collecterr.CodeOf(normalized) == collecterr.Canceled {
		return collecterr.Wrap(collecterr.Canceled, collecterr.ClassCanceled, cause)
	}

	if collecterr.CodeOf(normalized) == collecterr.Failed && collecterr.ClassOf(normalized) == collecterr.ClassTransient {
		return collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, cause)
	}

	if collecterr.IsUnclassified(normalized) {
		return unclassifiedRedacted(cause)
	}

	return collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, cause)
}

func RedactError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}

	text := err.Error()
	redacted := redactRequestText(text, secrets...)

	if redacted == text {
		return err
	}

	redactedErr := fmt.Errorf("%s", redacted)
	base := collecterr.Wrap(collecterr.CodeOf(err), collecterr.ClassOf(err), redactedErr)

	if collecterr.IsUnclassified(err) {
		base = unclassifiedRedacted(redactedErr)
	}

	if withRetryErr := collecterr.WithRetry(base, collecterr.RetryOf(err)); withRetryErr != nil {
		return fmt.Errorf("with retry: %w", withRetryErr)
	}

	return nil
}

// collecterr는 미분류 생성자를 노출하지 않는다. 평문 문자열 오류는 context/transient 판정에
// 걸리지 않으므로 FromContext가 항상 기본 버킷(Internal, unclassified)으로 떨어뜨린다.
func unclassifiedRedacted(redactedErr error) error {
	return collecterr.FromContext(redactedErr)
}

func redactRequestText(text string, secrets ...string) string {
	text = stripURLQuery(text)

	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}

	return collecterr.SanitizeDetail(text)
}
