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
	code  contract.CollectionErrorCode
	class contract.FailureClass
}

var fixedProviderStatuses = map[int]providerStatusClassification{
	http.StatusMovedPermanently:        {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusFound:                   {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusSeeOther:                {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusTemporaryRedirect:       {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusPermanentRedirect:       {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusBadRequest:              {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusNotFound:                {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusMethodNotAllowed:        {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusConflict:                {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusGone:                    {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusUnsupportedMediaType:    {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusUnprocessableEntity:     {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusUnauthorized:            {code: contract.ErrorConfiguration, class: contract.ClassConfiguration},
	http.StatusForbidden:               {code: contract.ErrorConfiguration, class: contract.ClassConfiguration},
	http.StatusRequestTimeout:          {code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	http.StatusTooEarly:                {code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	http.StatusInternalServerError:     {code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	http.StatusBadGateway:              {code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	http.StatusGatewayTimeout:          {code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	http.StatusNotImplemented:          {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
	http.StatusHTTPVersionNotSupported: {code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
}

func mapProviderStatus(provider contract.Provider, status int, retryAfter, diagnostic string) error {
	message := statusMessage(provider, status, diagnostic)
	if status == http.StatusOK {
		return nil
	}

	if status == http.StatusTooManyRequests {
		if err := withRetryHint(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), retryAfter); err != nil {
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
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassTransient, message)
	}

	return collecterr.New(contract.ErrorCollectionFailed, contract.ClassProtocol, message)
}

func mapServiceUnavailable(message, retryAfter string) error {
	hint := collecterr.ParseRetryAfter(retryAfter, time.Time{})
	if hint.Kind() == collecterr.RetryDefault {
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassTransient, message)
	}

	if err := collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), hint); err != nil {
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

	if collecterr.CodeOf(normalized) == contract.ErrorCollectionTimeout {
		return collecterr.Wrap(contract.ErrorCollectionTimeout, contract.ClassTimeout, cause)
	}

	if collecterr.CodeOf(normalized) == contract.ErrorCollectionCanceled {
		return collecterr.Wrap(contract.ErrorCollectionCanceled, contract.ClassCanceled, cause)
	}

	if collecterr.CodeOf(normalized) == contract.ErrorCollectionFailed && collecterr.ClassOf(normalized) == contract.ClassTransient {
		return collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, cause)
	}

	if collecterr.IsUnclassified(normalized) {
		return unclassifiedRedacted(cause)
	}

	return collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, cause)
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
