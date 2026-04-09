package providers

import (
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type InfraResources struct {
	CacheService     cache.Client
	PostgresService  database.Client
	MemberRepository *member.Repository
	MemberCache      *member.Cache
	CleanupCache     func()
	CleanupDB        func()
}

type YouTubeStack = sharedproviders.YouTubeStack
