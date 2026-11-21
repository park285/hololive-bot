package providerhttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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
		return withRetryHint(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), retryAfter)
	}

	if status == http.StatusServiceUnavailable {
		return mapServiceUnavailable(message, retryAfter)
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
	hint := collecterr.ParseRetryAfter(retryAfter)
	if hint.Kind() == collecterr.RetryDefault {
		return collecterr.New(contract.ErrorCollectionFailed, contract.ClassTransient, message)
	}

	return collecterr.WithRetry(collecterr.New(contract.ErrorCooldown, contract.ClassCooldown, message), hint)
}

func withRetryHint(err error, retryAfter string) error {
	return collecterr.WithRetry(err, collecterr.ParseRetryAfter(retryAfter))
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

	return collecterr.WithRetry(base, collecterr.RetryOf(err))
}

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
