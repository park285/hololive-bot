package migrationrunner

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/scripts/migrations"
)

// guardEpochResidue는 manifest 첫 항목을 epoch baseline으로 간주한다(위치 규약).
// 이 테스트는 그 규약을 checkpoint migration이 ledger에 선기록하는 리터럴과 묶는다:
// squash 후 manifest 첫 줄이 checkpoint 리터럴과 다르면 checkpoint를 거친 DB가
// 기동 거부되거나(fail-closed), 레거시 ledger에 이미 있는 이름이 오면 drift DB가
// 가드를 통과한다(fail-open). 어느 쪽이든 여기서 먼저 실패해야 한다.
func TestEpochCheckpointBaselineContract(t *testing.T) {
	const (
		baseline   = "001_schema_epoch2_baseline.sql"
		checkpoint = "140_epoch2_checkpoint.sql"
	)

	raw, err := fs.ReadFile(migrations.FS, checkpoint)
	if err != nil {
		t.Fatalf("read checkpoint migration: %v", err)
	}
	if !strings.Contains(string(raw), "'"+baseline+"'") {
		t.Fatalf("checkpoint %s must record epoch baseline %q into schema_migrations", checkpoint, baseline)
	}

	entries := manifestEntries(t)
	if entries[0] == baseline {
		return
	}
	if !containsEntry(entries, checkpoint) {
		t.Fatalf(
			"manifest first entry %q is not the epoch baseline %q and checkpoint %s is absent — "+
				"guardEpochResidue's entries[0] anchor no longer matches what the checkpoint recorded",
			entries[0], baseline, checkpoint)
	}
}
