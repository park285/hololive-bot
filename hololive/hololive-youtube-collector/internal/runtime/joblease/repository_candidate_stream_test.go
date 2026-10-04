package joblease

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 실제 scan을 한 번 성공시킨 다음 스트림 오류를 주입한다.
type interruptedCandidateRows struct {
	pgx.Rows

	read  bool
	cause error
}

func (r *interruptedCandidateRows) Next() bool {
	if r.read {
		return false
	}

	r.read = true

	return r.Rows.Next()
}

func (r *interruptedCandidateRows) Err() error { return r.cause }

func TestPartialCandidateStreamDoesNotReturnPage(t *testing.T) {
	pool := dbtest.NewPool(t)

	for _, maxMS := range []int64{60000, 120000} {
		rows, err := pool.Query(t.Context(), `SELECT true,'channel:a'::text,60000::bigint,$1::bigint`, maxMS)
		if err != nil {
			t.Fatal(err)
		}

		cause := errors.New("첫 후보 수신 후 연결 중단")
		job := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")
		page, err := collectCandidatePage(&interruptedCandidateRows{Rows: rows, cause: cause}, job, 1)
		rows.Close()

		if !errors.Is(err, cause) || errors.Is(err, ErrCandidateContract) || len(page.Jobs) != 0 {
			t.Fatalf("일부 후보 또는 runner-local 오류가 노출됐습니다: page=%v err=%v", page, err)
		}
	}
}
