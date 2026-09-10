// Package testaccountcli는 공개 HTTP 경로 없이 임시 조회 계정을 발급·조회·폐기합니다.
package testaccountcli

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/session"
)

type options struct {
	action   string
	output   string
	username string
	ttl      time.Duration
}

type receipt struct {
	Status        string `json:"status"`
	Username      string `json:"username,omitempty"`
	ExpiresAtUnix int64  `json:"expires_at_unix,omitempty"`
	ReadOnly      bool   `json:"read_only"`
}

// Run은 기존 설정의 Valkey 연결을 사용하고 30초의 I/O 예산 안에서 한 명령을 수행합니다.
// 자격증명은 명시한 비공개 파일에만 기록하며 출력에는 상태·식별자·만료만 포함합니다.
// 발급의 불명 결과에서는 파일을 보존하므로 같은 계정의 status/revoke로 먼저 확인해야 합니다.
func Run(ctx context.Context, args []string, output io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load test account configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	defer cancel()

	// 단발성 CLI는 캐시 조회를 사용하지 않으므로 client-side tracking 연결도 만들지 않습니다.
	store, err := session.NewStoreWithOptions(ctx, cfg.ValkeyURL, &cfg.Session, session.Options{DisableCache: true})
	if err != nil {
		return fmt.Errorf("connect test account store: %w", err)
	}
	defer store.Close()

	var result receipt

	switch opts.action {
	case "issue":
		result, err = issue(ctx, store, cfg, opts)
	case "status":
		result, err = status(ctx, store)
	case "revoke":
		result, err = revoke(ctx, store, opts.username)
	default:
		return errors.New("unsupported test account action")
	}

	if err != nil {
		return err
	}

	if err := jsonv2.MarshalWrite(output, result); err != nil {
		return fmt.Errorf("write test account receipt; inspect current state before retry: %w", err)
	}

	return nil
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New("test-account requires issue, status, or revoke")
	}

	opts := options{action: args[0]}
	flags := flag.NewFlagSet("test-account "+opts.action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	switch opts.action {
	case "issue":
		flags.DurationVar(&opts.ttl, "ttl", session.DefaultTestAccountTTL, "계정 유효 기간 (1~60분)")
		flags.StringVar(&opts.output, "credentials-file", "", "비공개 디렉터리의 새 자격증명 파일")
	case "status":
	case "revoke":
		flags.StringVar(&opts.username, "username", "", "폐기할 발급 식별자")
	default:
		return options{}, errors.New("test-account requires issue, status, or revoke")
	}

	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return options{}, errors.New("invalid test-account arguments")
	}

	return opts, opts.validate()
}

func (opts options) validate() error {
	if opts.action == "issue" && (!filepath.IsAbs(opts.output) || opts.ttl < time.Minute || opts.ttl > session.MaxTestAccountTTL || opts.ttl%time.Second != 0) {
		return errors.New("issue requires an absolute credentials-file and a whole-second TTL of 1~60 minutes")
	}

	if opts.action == "revoke" && opts.username == "" {
		return errors.New("revoke requires username")
	}

	return nil
}

func issue(ctx context.Context, store *session.Store, cfg *config.Config, opts options) (receipt, error) {
	cost, err := bcrypt.Cost([]byte(cfg.AdminPassHash))
	if err != nil {
		return receipt{}, errors.New("invalid administrator bcrypt cost")
	}

	account, credentials, err := session.NewTestAccount(opts.ttl, cost)
	if err != nil {
		return receipt{}, fmt.Errorf("prepare test account: %w", err)
	}

	if account.Username == cfg.AdminUser {
		return receipt{}, errors.New("generated test account conflicts with administrator")
	}

	// 저장 결과를 받지 못해도 같은 발급을 확인·폐기할 수 있도록 식별자와 자격증명을 먼저 보존합니다.
	if err := writeCredentials(opts.output, credentials); err != nil {
		return receipt{}, err
	}

	if err := store.IssueTestAccount(ctx, account); err != nil {
		return receipt{}, fmt.Errorf("issue test account; credentials file retained for reconciliation: %w", err)
	}

	return receipt{Status: "issued", Username: account.Username, ExpiresAtUnix: account.ExpiresAtUnix, ReadOnly: true}, nil
}

func status(ctx context.Context, store *session.Store) (receipt, error) {
	account, found, err := store.CurrentTestAccount(ctx)
	if err != nil {
		return receipt{}, fmt.Errorf("read current test account: %w", err)
	}

	if !found {
		return receipt{Status: "absent", ReadOnly: true}, nil
	}

	return receipt{Status: "active", Username: account.Username, ExpiresAtUnix: account.ExpiresAtUnix, ReadOnly: true}, nil
}

func revoke(ctx context.Context, store *session.Store, username string) (receipt, error) {
	changed, err := store.RevokeTestAccount(ctx, username)
	if err != nil {
		return receipt{}, fmt.Errorf("revoke test account: %w", err)
	}

	state := "absent"

	if changed {
		state = "revoked"
	}

	return receipt{Status: state, Username: username, ReadOnly: true}, nil
}

func writeCredentials(destination string, credentials session.TestCredentials) (err error) {
	root, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("open credentials directory: %w", err)
	}

	defer func() { err = errors.Join(err, root.Close()) }()

	parent, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open credentials directory metadata: %w", err)
	}

	info, statErr := parent.Stat()
	closeErr := parent.Close()

	if statErr != nil {
		return fmt.Errorf("inspect credentials directory: %w", errors.Join(statErr, closeErr))
	}

	if closeErr != nil {
		return fmt.Errorf("close credentials directory metadata: %w", closeErr)
	}

	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(metadata.Uid) != int64(os.Geteuid()) || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("credentials directory must be private and owned by the current user")
	}

	name := filepath.Base(destination)

	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create credentials file exclusively: %w", err)
	}

	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, root.Remove(name))
		}
	}()

	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("protect credentials file: %w", err)
	}

	if err := jsonv2.MarshalWrite(file, credentials); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync credentials file: %w", err)
	}

	return nil
}
