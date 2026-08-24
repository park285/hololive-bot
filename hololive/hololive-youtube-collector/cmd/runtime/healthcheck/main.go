package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/healthprobe"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

const externalSmokeURL = "https://www.google.com/generate_204"

func main() {
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "--body" {
		runBody(args[1])

		return
	}

	if len(args) == 3 && args[0] == "--body-api-key-env" {
		runBodyWithAPIKeyEnv(args[1], args[2])

		return
	}

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url>|--body <url>|--body-api-key-env <env> <url>|--smoke")
		os.Exit(2)
	}

	if args[0] == "--smoke" {
		runSmoke()

		return
	}

	runCheck(args[0])
}

func runBody(url string) {
	body, err := healthprobe.FetchURLInternal(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runBodyWithAPIKeyEnv(envName, url string) {
	body, err := fetchBodyWithAPIKeyEnv(envName, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fetchBodyWithAPIKeyEnv(envName, url string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return nil, errors.New("api key env name must not be empty")
	}

	apiKey := os.Getenv(envName)
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%s is empty or not set", envName)
	}

	out, err := healthprobe.FetchURLWithHeadersInternal(url, map[string]string{common.APIKeyHeader: apiKey})
	if err != nil {
		return out, fmt.Errorf("fetch URL with headers internal: %w", err)
	}

	return out, nil
}

func runCheck(url string) {
	if err := healthprobe.CheckURLInternal(url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runSmoke() {
	for _, name := range []string{"Asia/Seoul", "Asia/Tokyo", "UTC"} {
		if _, err := time.LoadLocation(name); err != nil {
			fmt.Fprintf(os.Stderr, "load location %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	if err := clearInternalTLSOverrides(); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)
		os.Exit(1)
	}

	if err := healthprobe.CheckURL(externalSmokeURL); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)
		os.Exit(1)
	}

	if _, err := fmt.Fprintln(os.Stdout, "smoke ok"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func clearInternalTLSOverrides() error {
	if err := os.Unsetenv(healthprobe.CACertFileEnv); err != nil {
		return fmt.Errorf("clear internal ca override: %w", err)
	}

	if err := os.Unsetenv(healthprobe.ServerNameEnv); err != nil {
		return fmt.Errorf("clear internal server name override: %w", err)
	}

	return nil
}
