package youtubejscollector

import (
	"testing"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/youtubejs"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestPaginationOfRejectsImpossibleTupleAsProtocolFault(t *testing.T) {
	t.Parallel()

	_, _, err := PaginationOf(&youtubejs.Pagination{
		PageCount:         1,
		Exhausted:         true,
		Continuity:        "",
		TerminationReason: youtubejs.TerminationExhausted,
	})
	if err == nil {
		t.Fatal("empty continuity must fail closed")
	}

	if collecterr.CodeOf(err) != contract.ErrorHelperProtocolMismatch {
		t.Fatalf("error code = %q, want helper protocol mismatch", collecterr.CodeOf(err))
	}
}
