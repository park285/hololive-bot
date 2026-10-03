package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoACLMode acl.ACLMode,
	kakaoRooms []string,
	postgres database.Client,
	logger *slog.Logger,
) (*acl.Service, error) {
	service, err := acl.NewACLService(
		ctx,
		postgres,
		logger,
		kakaoACLEnabled,
		kakaoACLMode,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACL service: %w", err)
	}

	return service, nil
}
