package grpcx

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	DefaultMaxRecvMsgSize = 16 * 1024 * 1024
	DefaultMaxSendMsgSize = 16 * 1024 * 1024
)

func DefaultClientDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(DefaultMaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(DefaultMaxSendMsgSize),
		),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   120 * time.Second,
			},
			MinConnectTimeout: 20 * time.Second,
		}),
	}
}

func DialInsecure(ctx context.Context, target string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := make([]grpc.DialOption, 0, 1+len(extra)+8)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	opts = append(opts, DefaultClientDialOptions()...)
	opts = append(opts, extra...)
	return grpc.DialContext(ctx, target, opts...) //nolint:staticcheck // DialContext는 legacy지만 안정적
}

func DialWithCreds(ctx context.Context, target string, creds credentials.TransportCredentials, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := make([]grpc.DialOption, 0, 1+len(extra)+8)
	opts = append(opts, grpc.WithTransportCredentials(creds))
	opts = append(opts, DefaultClientDialOptions()...)
	opts = append(opts, extra...)
	return grpc.DialContext(ctx, target, opts...) //nolint:staticcheck // DialContext는 legacy지만 안정적
}

func DefaultServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      2 * time.Hour,
			MaxConnectionAgeGrace: 5 * time.Minute,
			Time:                  2 * time.Hour,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(DefaultMaxRecvMsgSize),
		grpc.MaxSendMsgSize(DefaultMaxSendMsgSize),
	}
}
