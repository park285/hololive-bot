package internalhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedh3 "github.com/park285/shared-go/transport/h3"
	"github.com/park285/shared-go/v2/pkg/httputil"
)

func NewClientForURLStrict(rawURL string, timeout time.Duration, options sharedh3.ClientOptions) (*http.Client, error) {
	if !internalURLUsesHTTPS(rawURL) {
		return httputil.NewInternalServiceClient(timeout), nil
	}

	if err := validateInternalH3Options(options); err != nil {
		return nil, fmt.Errorf("load internal H3 client options for %s: %w", rawURL, err)
	}

	client, _, err := sharedh3.NewClient(timeout, options)
	if err != nil {
		return nil, fmt.Errorf("configure internal H3 client for %s: %w", rawURL, err)
	}

	return client, nil
}

func validateInternalH3Options(options sharedh3.ClientOptions) error {
	if strings.TrimSpace(options.CACertFile) == "" {
		return errors.New("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE is required")
	}

	if strings.TrimSpace(options.ServerName) == "" {
		return errors.New("HOLOLIVE_INTERNAL_H3_SERVER_NAME is required")
	}

	return nil
}

func internalURLUsesHTTPS(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed != nil && parsed.Scheme == "https"
}
