package youtubejscollector

import (
	"testing"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
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

	if collecterr.CodeOf(err) != collecterr.HelperProtocolMismatch {
		t.Fatalf("error code = %q, want helper protocol mismatch", collecterr.CodeOf(err))
	}
}
