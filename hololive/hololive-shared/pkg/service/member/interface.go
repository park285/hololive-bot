package member

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// DataProvider is an alias of domain.MemberDataProvider.
//
// Goal: allow service consumers to depend on member package interfaces, while keeping
// the canonical domain shape in one place.
type DataProvider = domain.MemberDataProvider

// CacheProvider exposes the minimal cache-warming behavior used at bootstrap time.
type CacheProvider interface {
	WarmUpCache(ctx context.Context) error
	Refresh(ctx context.Context) error
}

var _ DataProvider = (*ServiceAdapter)(nil)
var _ CacheProvider = (*Cache)(nil)
