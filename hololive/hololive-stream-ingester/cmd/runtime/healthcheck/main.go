package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--smoke" {
		runSmoke()
		return
	}

	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url>|--smoke")
		os.Exit(2)
	}

	if err := checkURL(os.Args[1]); err != nil {
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

	if err := checkURL("https://www.google.com"); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "smoke ok")
}

func checkURL(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s status: %d", url, resp.StatusCode)
	}
	return nil
}
