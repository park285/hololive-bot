// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package member

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

const (
	memberEpochAuthorityKey = "coord:member-cache:v2:epoch"
	memberEpochChannel      = "coord:member-cache:v2:epoch-notify"
	memberEpochDataPrefix   = "member-cache:v2:data:"
	memberEpochVersion      = 2
	maxMemberEpoch          = uint64(math.MaxInt64 - 1)
	epochOperationTimeout   = 5 * time.Second
	epochReconnectDelay     = time.Second

	epochReconcileStartup      = "startup"
	epochReconcilePeriodic     = "periodic"
	epochReconcileSubscription = "subscription"
	epochReconcileReconnect    = "reconnect"
	epochReconcileMutation     = "mutation"
)

type memberEpochNotification struct {
	Version int    `json:"version"`
	Epoch   uint64 `json:"epoch"`
}

type memberEpochAuthority interface {
	Current(context.Context) (uint64, error)
	Advance(context.Context) (uint64, error)
	Publish(context.Context, uint64) error
	Subscribe(context.Context, func(), func(string)) error
}

type valkeyMemberEpochAuthority struct {
	client valkey.Client
}

func newValkeyMemberEpochAuthority(client valkey.Client) *valkeyMemberEpochAuthority {
	return &valkeyMemberEpochAuthority{client: client}
}

func (a *valkeyMemberEpochAuthority) Current(ctx context.Context) (uint64, error) {
	value, err := a.client.Do(ctx, a.client.B().Get().Key(memberEpochAuthorityKey).Build()).ToString()
	if errors.Is(err, valkey.Nil) {
		if initErr := a.client.Do(ctx, a.client.B().Set().Key(memberEpochAuthorityKey).Value("1").Nx().Build()).Error(); initErr != nil && !errors.Is(initErr, valkey.Nil) {
			return 0, fmt.Errorf("initialize epoch: %w", initErr)
		}

		value, err = a.client.Do(ctx, a.client.B().Get().Key(memberEpochAuthorityKey).Build()).ToString()
	}

	if err != nil {
		return 0, fmt.Errorf("read epoch: %w", err)
	}

	out, err := parseMemberEpoch(value)
	if err != nil {
		return out, fmt.Errorf("parse member epoch: %w", err)
	}

	return out, nil
}

func (a *valkeyMemberEpochAuthority) Advance(ctx context.Context) (uint64, error) {
	epoch, err := a.client.Do(ctx, a.client.B().Incr().Key(memberEpochAuthorityKey).Build()).ToInt64()
	if err != nil {
		return 0, fmt.Errorf("increment epoch: %w", err)
	}

	if epoch <= 0 || uint64(epoch) > maxMemberEpoch {
		return 0, fmt.Errorf("increment epoch returned invalid value %d", epoch)
	}

	return uint64(epoch), nil
}

func (a *valkeyMemberEpochAuthority) Publish(ctx context.Context, epoch uint64) error {
	payload, err := jsonv2.Marshal(memberEpochNotification{Version: memberEpochVersion, Epoch: epoch})
	if err != nil {
		return fmt.Errorf("marshal epoch notification: %w", err)
	}

	if err := a.client.Do(ctx, a.client.B().Publish().Channel(memberEpochChannel).Message(string(payload)).Build()).Error(); err != nil {
		return fmt.Errorf("publish epoch notification: %w", err)
	}

	return nil
}

func (a *valkeyMemberEpochAuthority) Subscribe(ctx context.Context, onSubscribed func(), onMessage func(string)) error {
	subscribeCtx := valkey.WithOnSubscriptionHook(ctx, func(event valkey.PubSubSubscription) {
		if event.Kind == "subscribe" && event.Channel == memberEpochChannel && onSubscribed != nil {
			onSubscribed()
		}
	})

	if err := a.client.Receive(subscribeCtx, a.client.B().Subscribe().Channel(memberEpochChannel).Build(), func(message valkey.PubSubMessage) {
		if onMessage != nil {
			onMessage(message.Message)
		}
	}); err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	return nil
}

func parseMemberEpoch(value string) (uint64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return 0, errors.New("invalid member cache epoch encoding")
	}

	epoch, err := strconv.ParseUint(trimmed, 10, 63)
	if err != nil || epoch == 0 || epoch > maxMemberEpoch {
		return 0, errors.New("invalid member cache epoch value")
	}

	return epoch, nil
}

func (c *Cache) runEpochReconciliation(ctx context.Context) {
	triggers := make(chan string, 1)

	panicguard.Go(c.logger, "member-cache-epoch-reconciler", func() {
		c.runEpochReconcileWorker(ctx, triggers)
	})

	for ctx.Err() == nil {
		if c.runEpochSubscription(ctx, triggers) {
			return
		}

		if !waitForEpochReconnect(ctx) {
			return
		}
	}
}

func (c *Cache) runEpochSubscription(ctx context.Context, triggers chan<- string) bool {
	err := c.epoch.Subscribe(ctx, func() {
		signalEpochReconcile(triggers, epochReconcileSubscription)
	}, func(payload string) {
		c.handleEpochNotification(payload)
		signalEpochReconcile(triggers, epochReconcileSubscription)
	})
	if ctx.Err() != nil {
		return true
	}

	if c.logger != nil {
		c.logger.Warn("member cache epoch subscriber disconnected", slog.Any("error", err))
	}

	signalEpochReconcile(triggers, epochReconcileReconnect)

	return false
}

func waitForEpochReconnect(ctx context.Context) bool {
	timer := time.NewTimer(epochReconnectDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func signalEpochReconcile(triggers chan<- string, reason string) {
	select {
	case triggers <- reason:
	default:
	}
}

func (c *Cache) runEpochReconcileWorker(ctx context.Context, triggers <-chan string) {
	interval := c.epochReconcileInterval
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for {
		reason, ok := waitForEpochReconcile(ctx, ticker.C, triggers)
		if !ok {
			return
		}

		c.reconcileEpochWithTimeout(ctx, reason)
	}
}

func waitForEpochReconcile(ctx context.Context, ticks <-chan time.Time, triggers <-chan string) (string, bool) {
	select {
	case <-ctx.Done():
		return "", false
	case <-ticks:
		return epochReconcilePeriodic, true
	case reason := <-triggers:
		return reason, true
	}
}

func (c *Cache) handleEpochNotification(payload string) {
	var notification memberEpochNotification

	if err := jsonv2.Unmarshal([]byte(payload), &notification); err != nil || notification.Version != memberEpochVersion || notification.Epoch == 0 {
		memberCacheEpochNotificationsTotal.WithLabelValues("invalid").Inc()

		if c.logger != nil {
			c.logger.Warn("invalid member cache epoch notification; reconciling authority", slog.Any("error", err))
		}
	} else {
		memberCacheEpochNotificationsTotal.WithLabelValues("received").Inc()
	}
}

func (c *Cache) reconcileEpochWithTimeout(parent context.Context, reason string) {
	ctx, cancel := context.WithTimeout(parent, epochOperationTimeout)
	defer cancel()

	if err := c.reconcileEpoch(ctx, reason); err != nil && c.logger != nil {
		c.logger.Warn("member cache epoch reconciliation failed", slog.String("reason", reason), slog.Any("error", err))
	}
}

func (c *Cache) reconcileEpoch(ctx context.Context, reason string) error {
	c.epochMu.Lock()
	defer c.epochMu.Unlock()

	epoch, err := c.epoch.Current(ctx)
	if err != nil {
		c.markEpochUncertain(reason, err)

		return fmt.Errorf("current: %w", err)
	}

	if err := c.acceptEpoch(epoch, reason); err != nil {
		return fmt.Errorf("accept epoch: %w", err)
	}

	return nil
}

func (c *Cache) acceptEpoch(epoch uint64, reason string) error {
	previous := c.authorityEpoch.Load()
	if previous != 0 && epoch < previous {
		err := fmt.Errorf("member cache epoch regressed from %d to %d", previous, epoch)
		c.markEpochUncertain(reason, err)

		return err
	}

	c.applyEpoch(epoch, reason)

	return nil
}

func (c *Cache) applyEpoch(epoch uint64, reason string) {
	c.snapshotMu.Lock()

	previous := c.authorityEpoch.Load()
	if previous != epoch || !c.authorityHealthy.Load() {
		c.snapshotGeneration.Add(1)
		c.byChannelID.Clear()
		c.byName.Clear()
		c.allMembers.Clear()
		c.allMembersSnapshot.Store(nil)
		c.authorityEpoch.Store(epoch)
	}

	c.authorityHealthy.Store(true)
	c.snapshotMu.Unlock()
	memberCacheEpoch.Set(float64(epoch))
	memberCacheEpochReconcileTotal.WithLabelValues(reason, "success").Inc()
}

func (c *Cache) markEpochUncertain(reason string, err error) {
	c.snapshotMu.Lock()

	wasHealthy := c.authorityHealthy.Swap(false)
	if wasHealthy || c.allMembersSnapshot.Load() != nil {
		c.snapshotGeneration.Add(1)
		c.byChannelID.Clear()
		c.byName.Clear()
		c.allMembers.Clear()
		c.allMembersSnapshot.Store(nil)
	}

	c.snapshotMu.Unlock()
	memberCacheEpochReconcileTotal.WithLabelValues(reason, "failed").Inc()

	if c.logger != nil && err != nil {
		c.logger.Warn("member cache epoch authority uncertain; cache bypass enabled", slog.String("reason", reason), slog.Any("error", err))
	}
}

func (c *Cache) cacheBypassRequired(operation string) bool {
	if c == nil || c.epoch == nil || c.authorityHealthy.Load() {
		return false
	}

	memberCacheBypassTotal.WithLabelValues(operation, "epoch_unavailable").Inc()

	return true
}

func (c *Cache) distributedCacheUsable() bool {
	return c.cacheEnabled() && (c.epoch == nil || c.authorityHealthy.Load())
}

func (c *Cache) epochDataKey(legacyKey string) string {
	if c.epoch == nil {
		return legacyKey
	}

	return memberEpochDataPrefix + strconv.FormatUint(c.authorityEpoch.Load(), 10) + ":" + legacyKey
}

func (c *Cache) confirmEpochAfterLoad(ctx context.Context, generation uint64) error {
	if c.epoch == nil {
		return nil
	}

	c.epochMu.Lock()
	defer c.epochMu.Unlock()

	epoch, err := c.epoch.Current(ctx)
	if err != nil {
		c.markEpochUncertain("publish", err)

		return errAllMembersGenerationChanged
	}

	if epoch != c.authorityEpoch.Load() {
		if err := c.acceptEpoch(epoch, "publish"); err != nil {
			return errAllMembersGenerationChanged
		}

		return errAllMembersGenerationChanged
	}

	if c.snapshotGeneration.Load() != generation || !c.authorityHealthy.Load() {
		return errAllMembersGenerationChanged
	}

	return nil
}
