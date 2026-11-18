package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/transport/h3"

	dbtest "github.com/kapu/hololive-dbtest"
)

func migrate(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, "host=/var/run/postgresql user=dbtest dbname=dbtest sslmode=disable")
	if err != nil {
		return fmt.Errorf("open disposable socket: %w", err)
	}
	defer pool.Close()

	if err := dbtest.ApplyMigrations(ctx, pool); err != nil {
		return fmt.Errorf("migrate disposable database: %w", err)
	}

	return nil
}

func serveIris() error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/ready" {
			log.Print("GET /ready")
			w.WriteHeader(http.StatusOK)

			return
		}

		log.Print("rejected unexpected fixture request")
		http.NotFound(w, r)
	})

	server, err := h3.NewServer("127.0.0.1:3443", handler, "/fixture/cert.pem", "/fixture/key.pem")
	if err != nil {
		return fmt.Errorf("create Iris fixture: %w", err)
	}

	return server.ListenAndServe()
}

func endpoint(path string) (string, error) {
	switch {
	case strings.HasPrefix(path, "/api/holo/"):
		return "https://127.0.0.1:30006", nil
	case strings.HasPrefix(path, "/l/"):
		return "https://127.0.0.1:30001", nil
	case strings.HasPrefix(path, "/internal/alarm/"):
		return "https://127.0.0.1:30007", nil
	default:
		return "", errors.New("path outside verification routes")
	}
}

func request(ctx context.Context, method, path, expectedStatus, body string) error {
	client, closeClient, err := h3.NewClient(3*time.Second, h3.ClientOptions{CACertFile: "/fixture/cert.pem"})
	if err != nil {
		return fmt.Errorf("create H3 client: %w", err)
	}
	defer closeClient()

	ctx, cancel := context.WithTimeout(ctx, client.Timeout)
	defer cancel()

	base, err := endpoint(path)
	if err != nil {
		return err
	}

	target, err := url.ParseRequestURI(path)
	if err != nil {
		return fmt.Errorf("parse request path: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, base, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.URL.Path, req.URL.RawPath, req.URL.RawQuery = target.Path, target.RawPath, target.RawQuery
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", os.Getenv("C3_KEY"))

	resp, err := client.Transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("send H3 request: %w", err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("record response: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "%d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode)); err != nil {
		return fmt.Errorf("record status: %w", err)
	}

	if location := resp.Header.Get("Location"); location != "" {
		if _, err := fmt.Fprintf(os.Stderr, "Location: %q\n", location); err != nil {
			return fmt.Errorf("record location: %w", err)
		}
	}

	if fmt.Sprint(resp.StatusCode) != expectedStatus {
		return fmt.Errorf("expected %s, received %s", expectedStatus, resp.Status)
	}

	return nil
}

func run(ctx context.Context, args []string) error {
	switch {
	case len(args) == 1 && args[0] == "migrate":
		return migrate(ctx)
	case len(args) == 1 && args[0] == "iris":
		return serveIris()
	case len(args) == 4:
		return request(ctx, args[0], args[1], args[2], args[3])
	default:
		return errors.New("expected migrate or METHOD PATH STATUS BODY")
	}
}

func main() {
	log.SetFlags(0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)

	err := run(ctx, os.Args[1:])

	cancel()

	if err != nil {
		log.Fatal(err)
	}
}
