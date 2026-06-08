package polling

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

const globalBudgetReservationMemberSeparator = "|"

func globalBudgetReservationMember(class poller.BudgetBurstClass, ownerToken string) string {
	className := strings.TrimSpace(string(class))
	if className == "" {
		className = string(poller.BudgetBurstPrimary)
	}
	return className + globalBudgetReservationMemberSeparator + ownerToken
}

type globalBudgetKeys struct {
	BudgetPrefix      string
	ReservationPrefix string
	ClassInflight     string
	GlobalInflight    string
	Reservations      string
	Reservation       string
	SourceCooldown    string
	PrimaryInflight   string
	BackfillInflight  string
	FallbackInflight  string
}

func (l *globalBudgetLimiter) keys(source poller.BudgetSource, class poller.BudgetBurstClass, ownerToken string) globalBudgetKeys {
	return buildGlobalBudgetKeys(l.namespace, source, class, ownerToken)
}

func buildGlobalBudgetKeys(namespace string, source poller.BudgetSource, class poller.BudgetBurstClass, ownerToken string) globalBudgetKeys {
	sourceTag := string(source)
	budgetPrefix := fmt.Sprintf("hololive:%s:youtube-producer:budget:{%s}:", namespace, sourceTag)
	reservationPrefix := budgetPrefix + "reservation:"
	return globalBudgetKeys{
		BudgetPrefix:      budgetPrefix,
		ReservationPrefix: reservationPrefix,
		ClassInflight:     budgetPrefix + string(class) + ":inflight",
		GlobalInflight:    budgetPrefix + "global:inflight",
		Reservations:      budgetPrefix + "reservations",
		Reservation:       reservationPrefix + ownerToken,
		SourceCooldown:    fmt.Sprintf("hololive:%s:youtube-producer:source-cooldown:{%s}", namespace, sourceTag),
		PrimaryInflight:   budgetPrefix + string(poller.BudgetBurstPrimary) + ":inflight",
		BackfillInflight:  budgetPrefix + string(poller.BudgetBurstBackfill) + ":inflight",
		FallbackInflight:  budgetPrefix + string(poller.BudgetBurstFallback) + ":inflight",
	}
}

func sortedBudgetSources(sourceUnits map[poller.BudgetSource]float64) []poller.BudgetSource {
	sources := make([]poller.BudgetSource, 0, len(sourceUnits))
	for source := range sourceUnits {
		sources = append(sources, source)
	}
	sortBudgetSources(sources)
	return sources
}

func sortBudgetSources(sources []poller.BudgetSource) {
	for i := 1; i < len(sources); i++ {
		current := sources[i]
		j := i - 1
		for j >= 0 && string(sources[j]) > string(current) {
			sources[j+1] = sources[j]
			j--
		}
		sources[j+1] = current
	}
}

func (l *globalBudgetLimiter) newOwnerToken(job poller.BudgetJob) (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}
	material := strings.Join([]string{
		l.instanceID,
		job.Namespace,
		job.InstanceID,
		job.PollerName,
		job.ChannelID,
		job.JobKey,
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return sanitizeGlobalBudgetTokenPart(l.instanceID) + ":" + hex.EncodeToString(sum[:8]) + ":" + hex.EncodeToString(randomBytes[:]), nil
}

func sanitizeGlobalBudgetTokenPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if isGlobalBudgetTokenRune(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func isGlobalBudgetTokenRune(r rune) bool {
	if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
		return true
	}
	return strings.ContainsRune("_-.", r)
}

func normalizeGlobalBudgetNamespace(namespace string) string {
	normalized := strings.TrimSpace(namespace)
	if normalized == "" {
		return ""
	}
	normalized = strings.ToLower(normalized)
	normalized = strings.ReplaceAll(normalized, ":", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return normalized
}

func normalizeGlobalBudgetInstanceID(instanceID string) string {
	normalized := strings.TrimSpace(instanceID)
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func copySourceMaxInflight(source map[poller.BudgetSource]int) map[poller.BudgetSource]int {
	copied := make(map[poller.BudgetSource]int, len(source))
	maps.Copy(copied, source)
	return copied
}

func copyClassMaxInflight(class map[poller.BudgetBurstClass]int) map[poller.BudgetBurstClass]int {
	copied := make(map[poller.BudgetBurstClass]int, len(class))
	maps.Copy(copied, class)
	return copied
}

func durationMillis(ttl time.Duration) int64 {
	ms := ttl.Milliseconds()
	if ms <= 0 {
		return 1
	}
	return ms
}

func millisDuration(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
