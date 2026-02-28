package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/util"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

func main() {
	logger := util.NewLoggerWithLevel("info")

	log.Println("=== PostgreSQL Member Data Integration Test ===")
	log.Println()

	// Initialize PostgreSQL
	postgresCfg := config.PostgresConfig{
		Host:     envOrDefault("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:     envOrDefaultInt("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		User:     envOrDefault("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password: envOrDefault("POSTGRES_PASSWORD", constants.DatabaseDefaults.Password),
		Database: envOrDefault("POSTGRES_DB", constants.DatabaseDefaults.Database),
	}

	buildCtx, buildCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Build)
	runtime, err := app.BuildDBIntegrationRuntime(buildCtx, postgresCfg, logger)
	buildCancel()
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer runtime.Close()
	log.Println("PostgreSQL connected")

	// Initialize Repository
	repo := runtime.Repository
	log.Println("Repository created")

	// Test 1: Get all members
	ctx := context.Background()
	members, err := repo.GetAllMembers(ctx)
	if err != nil {
		log.Fatalf("Failed to get all members: %v", err)
	}
	log.Printf("Loaded %d members from PostgreSQL", len(members))

	// Test 2: Find by channel ID
	testChannelID := "UChAnqc_AY5_I3Px5dig3X1Q" // Korone
	foundMember, err := repo.FindByChannelID(ctx, testChannelID)
	if err != nil {
		log.Fatalf("Failed to find by channel ID: %v", err)
	}
	if foundMember == nil {
		log.Fatal("Korone not found")
	}
	log.Printf("Find by channel ID: %s (aliases: ko=%d, ja=%d)",
		foundMember.Name, len(foundMember.Aliases.Ko), len(foundMember.Aliases.Ja))

	// Test 3: Find by alias
	foundMember, err = repo.FindByAlias(ctx, "코로네")
	if err != nil {
		log.Fatalf("Failed to find by alias: %v", err)
	}
	if foundMember == nil {
		log.Fatal("Alias '코로네' not found")
	}
	log.Printf("Find by alias '코로네': %s", foundMember.Name)

	// Test 4: Initialize Cache (without Valkey)
	memberCache := runtime.Cache
	log.Println("Cache created with warm-up")

	// Test 5: Cache queries
	foundMember, err = memberCache.GetByChannelID(ctx, testChannelID)
	if err != nil {
		log.Fatalf("Cache GetByChannelID failed: %v", err)
	}
	if foundMember == nil {
		log.Fatal("Korone not in cache")
	}
	log.Printf("Cache hit: %s", foundMember.Name)

	// Test 6: Adapter
	adapter := runtime.MemberAdapter
	adapterCtx := adapter.WithContext(ctx)
	foundMember = adapterCtx.FindMemberByChannelID(testChannelID)
	if foundMember == nil {
		log.Fatal("Adapter failed")
	}
	log.Printf("Adapter works: %s", foundMember.Name)

	channelIDs := adapterCtx.GetChannelIDs()
	log.Printf("Adapter GetChannelIDs: %d channels", len(channelIDs))

	allMembers := adapterCtx.GetAllMembers()
	log.Printf("Adapter GetAllMembers: %d members", len(allMembers))

	log.Println()
	log.Println("=== ALL TESTS PASSED ===")
	log.Println()
	fmt.Println("Summary:")
	fmt.Printf("- Total members: %d\n", len(members))
	fmt.Printf("- With channel ID: %d\n", len(channelIDs))
	fmt.Printf("- Repository: OK\n")
	fmt.Printf("- Cache: OK\n")
	fmt.Printf("- Adapter: OK\n")
	fmt.Printf("- Alias search: OK\n")
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("⚠ Invalid value for %s (%s), using default %d\n", key, value, fallback)
		return fallback
	}
	return parsed
}

// 린터 경고 억제: 사용된 인터페이스
var _ = slog.Default
