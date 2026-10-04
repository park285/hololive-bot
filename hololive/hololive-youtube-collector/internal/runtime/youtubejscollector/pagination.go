package youtubejscollector

import (
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// maxResultsPerPage는 community·content 목록 요청의 고정 항목 수입니다.
const maxResultsPerPage = 10

func PaginationOf(page *youtubejs.Pagination) (contract.Completeness, contract.Continuity, error) {
	if page == nil {
		return "", "", collecterr.New(collecterr.Internal, collecterr.ClassInternal, "pagination is nil")
	}

	completeness, continuity, err := page.Quality()
	if err != nil {
		return "", "", collecterr.Wrap(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, err)
	}

	return completeness, continuity, nil
}
