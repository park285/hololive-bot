package youtubejscollector

import (
	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/youtubejs"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const maxResultsPerPage = 10

func PaginationOf(page *youtubejs.Pagination) (contract.Completeness, contract.Continuity, error) {
	if page == nil {
		return "", "", collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "pagination is nil")
	}

	completeness, continuity, err := page.Quality()
	if err != nil {
		return "", "", collecterr.Wrap(contract.ErrorHelperProtocolMismatch, contract.ClassProtocol, err)
	}

	return completeness, continuity, nil
}
