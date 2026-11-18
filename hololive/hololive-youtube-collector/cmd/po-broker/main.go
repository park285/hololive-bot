package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/pobroker"
)

var (
	revision = "unknown"
	version  = "dev"
)

var (
	errListenFD     = errors.New("invalid inherited listener descriptor")
	errNotUnix      = errors.New("inherited listener is not a Unix listener")
	errNotSocket    = errors.New("socket path is not a socket")
	errSocketActive = errors.New("socket already has a listener")
)

func main() { os.Exit(run(os.Args[1:])) }

type options struct {
	listenFD       int
	socket         string
	node           string
	worker         string
	startupTimeout time.Duration
	showVersion    bool
	healthcheck    bool
}

func parseOptions(args []string) (*options, bool) {
	var opts options

	flags := flag.NewFlagSet("po-broker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&opts.listenFD, "listen-fd", 0, "inherited Unix listener descriptor")
	flags.StringVar(&opts.socket, "socket", "", "Unix socket")
	flags.StringVar(&opts.node, "node", "/usr/local/bin/node", "Node executable")
	flags.StringVar(&opts.worker, "worker", "/app/po-sandbox/src/worker.mjs", "isolated worker")
	flags.DurationVar(&opts.startupTimeout, "startup-timeout", 30*time.Second, "worker loaded timeout (positive, at most 90s)")
	flags.BoolVar(&opts.showVersion, "version", false, "print build identity")
	flags.BoolVar(&opts.healthcheck, "healthcheck", false, "check broker over Unix socket")

	if flags.Parse(args) != nil || flags.NArg() != 0 || opts.startupTimeout <= 0 || opts.startupTimeout > 90*time.Second {
		return nil, false
	}

	return &opts, true
}

func run(args []string) int {
	opts, ok := parseOptions(args)
	if !ok {
		return 2
	}

	if opts.showVersion {
		return printVersion(args)
	}

	if opts.healthcheck {
		return healthcheckStatus(opts.socket, opts.listenFD)
	}

	if (opts.listenFD == 0) == (opts.socket == "") || (opts.listenFD != 0 && opts.listenFD != 3) {
		return 2
	}

	broker := pobroker.New(revision, opts.node, opts.worker)
	report := exitReporter(os.Stderr, broker.Generation())
	stopWatching := watchTermination(report, broker.ExitReason)

	listener, err := prepareListener(context.Background(), opts.socket, opts.listenFD)
	if err != nil {
		report(pobroker.ExitListenerFailed)
		stopWatching()

		return 1
	}

	serveErr := broker.Serve(listener, opts.startupTimeout)
	if broker.ExitReason() == pobroker.ExitStartupFailed {
		reportStartupFailure(os.Stderr, serveErr)
	}

	report(broker.ExitReason())
	stopWatching()

	closeErr := listener.Close()
	if serveErr != nil || (closeErr != nil && !errors.Is(closeErr, net.ErrClosed)) {
		return 1
	}

	return 0
}

func reportStartupFailure(w io.Writer, err error) {
	classification := "unclassified"

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		classification = "deadline"
	case errors.Is(err, context.Canceled):
		classification = "canceled"
	}

	if _, writeErr := fmt.Fprintf(w, "po-broker startup_failure=%s\n", classification); writeErr != nil {
		return
	}
}

func exitReporter(w io.Writer, generation string) func(pobroker.ExitReason) {
	var once sync.Once

	return func(reason pobroker.ExitReason) {
		once.Do(func() {
			if _, err := fmt.Fprintf(w, "po-broker exit reason=%s generation=%s\n", reason, generation); err != nil {
				return
			}
		})
	}
}

func watchTermination(report func(pobroker.ExitReason), retired func() pobroker.ExitReason) (stop func()) {
	watched := []os.Signal{syscall.SIGTERM}

	if !signal.Ignored(syscall.SIGINT) {
		watched = append(watched, syscall.SIGINT)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, watched...)

	go func() {
		received := <-signals
		sig := syscall.SIGTERM

		if received == syscall.SIGINT {
			sig = syscall.SIGINT
		}

		reason := retired()
		if reason == "" {
			reason = pobroker.ExitSignal
		}

		report(reason)
		signal.Reset(syscall.SIGTERM, syscall.SIGINT)

		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			os.Exit(1)
		}
	}()

	return func() { signal.Stop(signals) }
}

func printVersion(args []string) int {
	if len(args) != 1 {
		return 2
	}

	if json.MarshalWrite(os.Stdout, struct {
		Revision string `json:"revision"`
		Version  string `json:"version"`
		GOOS     string `json:"goos"`
		GOARCH   string `json:"goarch"`
	}{revision, version, runtime.GOOS, runtime.GOARCH}) != nil {
		return 1
	}

	if _, err := fmt.Fprintln(os.Stdout); err != nil {
		return 1
	}

	return 0
}

func healthcheckStatus(socket string, listenFD int) int {
	if socket == "" || listenFD != 0 {
		return 2
	}

	if check(socket) != nil {
		return 1
	}

	return 0
}

func prepareListener(ctx context.Context, socket string, listenFD int) (net.Listener, error) {
	if listenFD != 0 {
		return inheritedListener(listenFD)
	}

	return socketListener(ctx, socket)
}

func inheritedListener(listenFD int) (net.Listener, error) {
	file := os.NewFile(uintptr(listenFD), "po-listener")
	if file == nil {
		return nil, errListenFD
	}

	listener, err := net.FileListener(file)
	closeErr := file.Close()

	if err != nil {
		return nil, fmt.Errorf("inherit listener: %w", errors.Join(err, closeErr))
	}

	if closeErr != nil {
		return nil, fmt.Errorf("close inherited descriptor: %w", errors.Join(closeErr, listener.Close()))
	}

	if _, ok := listener.(*net.UnixListener); !ok {
		return nil, errors.Join(errNotUnix, listener.Close())
	}

	return listener, nil
}

func socketListener(ctx context.Context, socket string) (net.Listener, error) {
	if err := removeStaleSocket(ctx, socket); err != nil {
		return nil, err
	}

	var config net.ListenConfig

	listener, err := config.Listen(ctx, "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen socket: %w", err)
	}

	if err = os.Chmod(socket, 0o660); err != nil { // #nosec G302 -- 교차 UID 소켓 IPC에 group 접근이 필수다
		return nil, fmt.Errorf("chmod socket: %w", errors.Join(err, listener.Close()))
	}

	return listener, nil
}

func removeStaleSocket(ctx context.Context, socket string) error {
	info, err := os.Lstat(socket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect socket: %w", err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return errNotSocket
	}

	dialer := net.Dialer{Timeout: 200 * time.Millisecond}

	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err == nil {
		return errors.Join(errSocketActive, conn.Close())
	}

	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe socket: %w", err)
	}

	if err = os.Remove(socket); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	return nil
}

func check(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}

	defer transport.CloseIdleConnections()

	//nolint:revive // unsecure-url-scheme: 고정 DialContext로 AF_UNIX에만 연결하므로 호스트는 무시되며 https는 broker에 없는 TLS를 요구한다.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/health", http.NoBody)
	if err != nil {
		return err
	}

	response, err := (&http.Client{
		Transport: transport, Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK || response.ContentLength > 4096 {
		return errors.New("unhealthy")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return errors.New("invalid health")
	}

	var value struct {
		ProtocolVersion int            `json:"protocol_version"`
		Generation      string         `json:"generation"`
		State           pobroker.State `json:"state"`
		Revision        string         `json:"revision"`
	}

	if json.Unmarshal(data, &value, json.RejectUnknownMembers(true)) != nil ||
		value.ProtocolVersion != 1 || value.Generation == "" || value.Revision != revision {
		return errors.New("invalid health")
	}

	switch value.State {
	case pobroker.Idle, pobroker.Starting, pobroker.AwaitingChallenge, pobroker.AwaitingIntegrity, pobroker.Ready:
		return nil
	}

	return errors.New("invalid health")
}
