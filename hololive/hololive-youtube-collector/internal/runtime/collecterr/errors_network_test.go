package collecterr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// DB 장애(연결 거부·재시작 중 SQLSTATE 57P03·연결 dial 타임아웃)는 코드 결함이 아니므로 INTERNAL로 집계되면 안 된다.
// 반대로 인증 실패(28P01)는 설정 결함이라 일시 장애로 숨기지 않고 미분류로 남는다.
func TestDatabaseConnectFailuresClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		code         contract.CollectionErrorCode
		class        contract.FailureClass
		unclassified bool
	}{
		{name: "connection refused", err: connectRefusedError(t), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "database shutting down 57P03", err: connectRejectedError(t, "57P03"), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "admin shutdown 57P01", err: connectRejectedError(t, "57P01"), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "connection failure 08006", err: connectRejectedError(t, "08006"), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "dial poll deadline", err: pollDeadlineDialError(), code: contract.ErrorCollectionTimeout, class: contract.ClassTimeout},
		{name: "read socket deadline stays unclassified", err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, code: contract.ErrorInternalInvariant, class: contract.ClassInternal, unclassified: true},
		{name: "authentication failure 28P01 stays unclassified", err: connectRejectedError(t, "28P01"), code: contract.ErrorInternalInvariant, class: contract.ClassInternal, unclassified: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("acquire collection lease: begin pgx transaction: %w", test.err)

			for name, err := range map[string]error{"normalize": wrapped, "fromContext": FromContext(wrapped)} {
				if CodeOf(err) != test.code || ClassOf(err) != test.class {
					t.Fatalf("%s: CodeOf/ClassOf = %s/%s, want %s/%s (err=%v)", name, CodeOf(err), ClassOf(err), test.code, test.class, err)
				}

				if IsUnclassified(err) != test.unclassified {
					t.Fatalf("%s: IsUnclassified = %v, want %v (err=%v)", name, IsUnclassified(err), test.unclassified, err)
				}
			}
		})
	}
}

// connectRefusedError는 닫힌 포트로 pgconn 연결을 시도해 실제 ConnectError를 얻는다.
func connectRefusedError(t *testing.T) error {
	t.Helper()

	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	return connectError(t, addr)
}

// connectRejectedError는 startup 직후 code SQLSTATE의 FATAL 오류를 돌려주는 PostgreSQL 대역 서버에 연결한다.
func connectRejectedError(t *testing.T, code string) error {
	t.Helper()

	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close fake postgres listener: %v", err)
		}
	})

	served := make(chan error, 1)

	go func() {
		served <- rejectStartup(listener, code)
	}()

	connectErr := connectError(t, listener.Addr().String())

	if err := <-served; err != nil {
		t.Fatalf("fake postgres: %v", err)
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](connectErr); !ok || pgErr.Code != code {
		t.Fatalf("connect error = %v, want PgError %s", connectErr, code)
	}

	return connectErr
}

func rejectStartup(listener net.Listener, code string) error {
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}

	return errors.Join(sendStartupRejection(conn, code), conn.Close())
}

func sendStartupRejection(conn net.Conn, code string) error {
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return fmt.Errorf("receive startup: %w", err)
	}

	backend.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: code, Message: "rejected by test server"})

	if err := backend.Flush(); err != nil {
		return fmt.Errorf("flush error response: %w", err)
	}

	return nil
}

func connectError(t *testing.T, addr string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	conn, err := pgconn.Connect(ctx, "postgres://collector@"+addr+"/hololive?sslmode=disable")
	if err == nil {
		t.Fatalf("pgconn.Connect succeeded, want failure (close: %v)", conn.Close(ctx))
	}

	if _, ok := errors.AsType[*pgconn.ConnectError](err); !ok {
		t.Fatalf("connect error = %T %v, want *pgconn.ConnectError", err, err)
	}

	return err
}

// pollDeadlineDialError는 연결 대기의 poll deadline이 context 타이머보다 먼저 끝났을 때 net이 돌려주는 dial 오류다.
// 이때 원인은 context.DeadlineExceeded가 아닌 os.ErrDeadlineExceeded이고, pgconn은 이를 dial 오류로 감싼다.
func pollDeadlineDialError() error {
	return fmt.Errorf("dial error: %w", &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded})
}
