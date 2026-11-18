package internalhttp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

func CloseClient(client *http.Client) error {
	if client == nil {
		return nil
	}

	closer, ok := client.Transport.(io.Closer)
	if !ok {
		return nil
	}

	if err := closer.Close(); err != nil {
		return fmt.Errorf("close internal http transport: %w", err)
	}

	return nil
}

func CloseAll(clients ...io.Closer) error {
	var errs []error

	for _, client := range clients {
		if client == nil {
			continue
		}

		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
