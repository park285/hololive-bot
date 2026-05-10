package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
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

	client := http.DefaultClient
	parsed, err := parseURL(url)
	if err != nil {
		return err
	}
	if parsed.Scheme == "https" {
		h3Client, closeFn, err := newHTTP3HealthClient()
		if err != nil {
			return err
		}
		defer closeFn()
		client = h3Client
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
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

func parseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	return parsed, nil
}

func newHTTP3HealthClient() (*http.Client, func(), error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: os.Getenv("HEALTHCHECK_SERVER_NAME"),
	}
	if caFile := os.Getenv("HEALTHCHECK_CA_CERT_FILE"); caFile != "" {
		roots, err := loadRootCAs(caFile)
		if err != nil {
			return nil, nil, err
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http3.Transport{
		TLSClientConfig: tlsConfig,
		QUICConfig: &quic.Config{
			InitialPacketSize: 1200,
		},
	}
	return &http.Client{Transport: transport}, func() { _ = transport.Close() }, nil
}

func loadRootCAs(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read healthcheck CA file: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("read healthcheck CA file: no PEM certificates in %s", path)
	}
	return roots, nil
}
