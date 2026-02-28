package valkeyx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Config: Valkey 클라이언트 연결에 필요한 설정 정보를 담고 있다.
type Config struct {
	Addr         string
	Username     string
	Password     string //nolint:gosec // 설정 구조체 필드명이며 시크릿 값 자체를 로그/출력하지 않는다.
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	PoolSize     int
	MinIdleConns int

	// DisableCache: 클라이언트 사이드 캐싱(Client Side Caching) 기능을 비활성화할지 여부.
	// 일반적으로 로컬 테스트 환경이나 miniredis 사용 시 true로 설정합니다.
	DisableCache bool

	// UseTLS: TLS(SSL) 연결 사용 여부.
	UseTLS bool

	// SocketPath: Unix Domain Socket 경로.
	// 비어있지 않으면 Addr 대신 UDS로 연결합니다.
	SocketPath string
}

// NewClient: 주어진 설정을 바탕으로 Valkey 클라이언트 인스턴스를 생성하고 초기화합니다.
// SocketPath가 설정되면 UDS로 연결하고, 비어있으면 TCP로 연결합니다.
func NewClient(cfg Config) (valkey.Client, error) {
	socketPath := strings.TrimSpace(cfg.SocketPath)
	addr := strings.TrimSpace(cfg.Addr)

	// UDS 모드가 아닌 경우 addr 필수
	if socketPath == "" && addr == "" {
		return nil, errors.New("valkey addr is empty and socket path not set")
	}

	var tlsConfig *tls.Config
	if cfg.UseTLS && socketPath == "" {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		}
	}

	// InitAddress 설정: UDS 모드에서도 필요 (valkey-go 내부 구조상)
	initAddr := addr
	if socketPath != "" {
		// UDS 모드에서는 소켓 경로를 주소로 사용
		initAddr = socketPath
	}

	opts := valkey.ClientOption{
		InitAddress:  []string{initAddr},
		Username:     cfg.Username,
		Password:     cfg.Password,
		SelectDB:     cfg.DB,
		TLSConfig:    tlsConfig,
		DisableCache: cfg.DisableCache,
	}

	// UDS 모드: 커스텀 DialCtxFn 설정
	if socketPath != "" {
		opts.DialCtxFn = func(ctx context.Context, _ string, _ *net.Dialer, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			if cfg.DialTimeout > 0 {
				d.Timeout = cfg.DialTimeout
			}
			return d.DialContext(ctx, "unix", socketPath)
		}
	} else if cfg.DialTimeout > 0 {
		// TCP 모드: Timeout 설정
		opts.Dialer.Timeout = cfg.DialTimeout
	}

	// PoolSize/MinIdleConns는 BlockingPool 설정으로 매핑합니다.
	if cfg.PoolSize > 0 {
		opts.BlockingPoolSize = cfg.PoolSize
	}
	if cfg.MinIdleConns > 0 {
		opts.BlockingPoolMinSize = cfg.MinIdleConns
	}

	connTimeout := max(cfg.WriteTimeout, cfg.ReadTimeout)
	if connTimeout > 0 {
		opts.ConnWriteTimeout = connTimeout
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("create valkey client failed: %w", err)
	}

	return client, nil
}

// Ping: Valkey 서버와의 연결 상태를 점검합니다. (PING 명령 전송)
func Ping(ctx context.Context, client valkey.Client) error {
	if client == nil {
		return errors.New("valkey client is nil")
	}
	cmd := client.B().Ping().Build()
	if err := client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("valkey ping failed: %w", err)
	}
	return nil
}

// IsNil: 발생한 에러가 Valkey nil(키가 없음) 에러인지 확인합니다.
// 에러 래핑을 고려하여 언래핑 후 검사를 수행합니다.
func IsNil(err error) bool {
	if valkey.IsValkeyNil(err) {
		return true
	}
	// fmt.Errorf("%w", err)로 래핑된 경우 언래핑하여 체크
	unwrapped := err
	for unwrapped != nil {
		if valkey.IsValkeyNil(unwrapped) {
			return true
		}
		unwrapped = errors.Unwrap(unwrapped)
	}
	return false
}

// IsNoScript: Lua 스크립트 SHA가 서버에 존재하지 않을 때 발생하는 NOSCRIPT 에러인지 확인합니다.
// 에러 래핑을 고려하여 언래핑 후 검사를 수행합니다.
func IsNoScript(err error) bool {
	return containsValkeyErrorPrefix(err, "NOSCRIPT")
}

// IsBusyGroup: 소비자 그룹이 이미 존재할 때 발생하는 BUSYGROUP 에러인지 확인합니다.
// Redis Streams의 XGROUP CREATE 명령어 실행 시 발생할 수 있습니다.
func IsBusyGroup(err error) bool {
	return containsValkeyErrorPrefix(err, "BUSYGROUP")
}

// containsValkeyErrorPrefix: Valkey 에러 메시지가 특정 접두사로 시작하는지 확인합니다.
// valkey-go의 ValkeyError 타입을 활용하여 에러 체크를 수행합니다.
func containsValkeyErrorPrefix(err error, prefix string) bool {
	if err == nil {
		return false
	}

	// ValkeyError 타입으로 변환하여 IsNoScript/IsBusyGroup 등의 메서드 활용
	var valkeyErr *valkey.ValkeyError
	if errors.As(err, &valkeyErr) {
		// ValkeyError는 접두사 기반 체크를 지원
		return valkeyErr.IsNoScript() && prefix == "NOSCRIPT" ||
			valkeyErr.IsBusyGroup() && prefix == "BUSYGROUP"
	}

	// fallback: 문자열 기반 체크 (래핑된 에러 등)
	return strings.Contains(err.Error(), prefix)
}

// Close: Valkey 클라이언트 연결을 안전하게 종료합니다.
func Close(client valkey.Client) {
	if client != nil {
		client.Close()
	}
}

// GetBytes: Valkey에서 key 값을 bytes로 조회합니다.
// 키가 없으면 (nil, false, nil)을 반환합니다.
func GetBytes(ctx context.Context, client valkey.Client, key string) ([]byte, bool, error) {
	if client == nil {
		return nil, false, errors.New("valkey client is nil")
	}

	cmd := client.B().Get().Key(key).Build()
	raw, err := client.Do(ctx, cmd).AsBytes()
	if err != nil {
		if IsNil(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("valkey get bytes failed: %w", err)
	}
	return raw, true, nil
}

// SetStringEX: Valkey에 값을 저장하고 ttl이 0보다 크면 TTL을 설정합니다.
func SetStringEX(ctx context.Context, client valkey.Client, key string, value string, ttl time.Duration) error {
	if client == nil {
		return errors.New("valkey client is nil")
	}

	var cmd valkey.Completed
	if ttl > 0 {
		cmd = client.B().Set().Key(key).Value(value).Ex(ttl).Build()
	} else {
		cmd = client.B().Set().Key(key).Value(value).Build()
	}

	if err := client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("valkey set string failed: %w", err)
	}
	return nil
}

// DeleteKeys: 지정한 key 목록을 삭제합니다.
func DeleteKeys(ctx context.Context, client valkey.Client, keys ...string) error {
	if client == nil {
		return errors.New("valkey client is nil")
	}
	if len(keys) == 0 {
		return nil
	}

	cmd := client.B().Del().Key(keys...).Build()
	if err := client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("valkey delete keys failed: %w", err)
	}
	return nil
}
