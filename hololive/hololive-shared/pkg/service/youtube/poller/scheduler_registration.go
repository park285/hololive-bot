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

// Package poller: YouTube 채널 데이터 폴링 및 스케줄링
package poller

import (
	"container/heap"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (s *Scheduler) Register(channelID string, poller Poller, priority Priority, interval time.Duration) {
	if err := s.RegisterChecked(channelID, poller, priority, interval); err != nil {
		slog.Warn("Skip invalid scheduler registration",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
	}
}

func (s *Scheduler) RegisterChecked(channelID string, poller Poller, priority Priority, interval time.Duration) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return fmt.Errorf("channel id is empty")
	}
	if poller == nil {
		return fmt.Errorf("poller is nil")
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be positive: %s", interval)
	}

	pollerName := strings.TrimSpace(poller.Name())
	if pollerName == "" {
		return fmt.Errorf("poller name is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := channelID + ":" + pollerName
	if _, exists := s.jobMap[key]; exists {
		return nil // 중복 등록 방지
	}

	offset := calculateOffset(key, interval)
	job := &Job{
		ChannelID: channelID,
		Poller:    poller,
		Priority:  priority,
		NextRunAt: nextPollAt(time.Now(), interval, offset),
		Interval:  interval,
		Offset:    offset,
		key:       key,
	}

	heap.Push(&s.jobs, job)
	s.jobMap[key] = job
	schedulerRegisteredJobs.Set(float64(len(s.jobMap)))
	s.notifyDispatcher()
	return nil
}

func (s *Scheduler) UpdatePriority(channelID string, pollerName string, priority Priority, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := channelID + ":" + pollerName
	job, exists := s.jobMap[key]
	if !exists {
		return
	}

	job.Priority = priority
	if job.Interval != interval && interval > 0 {
		s.resetJobScheduleForIntervalChange(job, interval)
	}
	job.Interval = interval
	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	}
	s.notifyDispatcher()
}

func (s *Scheduler) SyncPollerTargets(targetSync PollerTargetSync) {
	if targetSync.Poller == nil || targetSync.Interval <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pollerName := strings.TrimSpace(targetSync.Poller.Name())
	if pollerName == "" {
		return
	}

	desired := make(map[string]struct{}, len(targetSync.ChannelIDs))
	for _, channelID := range targetSync.ChannelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			continue
		}
		desired[channelID] = struct{}{}
	}

	for key, job := range s.jobMap {
		if job == nil || job.Poller == nil || job.Poller.Name() != pollerName {
			continue
		}

		if _, keep := desired[job.ChannelID]; !keep {
			job.retired = true
			if job.index >= 0 {
				heap.Remove(&s.jobs, job.index)
			}
			delete(s.jobMap, key)
			continue
		}

		job.Poller = targetSync.Poller
		job.Priority = targetSync.Priority
		if job.Interval != targetSync.Interval {
			s.resetJobScheduleForIntervalChange(job, targetSync.Interval)
		}
		job.Interval = targetSync.Interval
		if job.index >= 0 {
			heap.Fix(&s.jobs, job.index)
		}
		delete(desired, job.ChannelID)
	}

	now := time.Now()
	for channelID := range desired {
		key := channelID + ":" + pollerName
		offset := calculateOffset(key, targetSync.Interval)
		nextRunAt := nextPollAt(now, targetSync.Interval, offset)
		if targetSync.ForceImmediateFirstRun {
			nextRunAt = now
		}
		job := &Job{
			ChannelID:         channelID,
			Poller:            targetSync.Poller,
			Priority:          targetSync.Priority,
			NextRunAt:         nextRunAt,
			Interval:          targetSync.Interval,
			Offset:            offset,
			key:               key,
			immediateFirstRun: targetSync.ForceImmediateFirstRun,
		}
		heap.Push(&s.jobs, job)
		s.jobMap[key] = job
	}

	schedulerRegisteredJobs.Set(float64(len(s.jobMap)))
	s.notifyDispatcher()
}

func (s *Scheduler) resetJobScheduleForIntervalChange(job *Job, interval time.Duration) {
	if job == nil || interval <= 0 {
		return
	}

	job.consecutiveFailures = 0
	job.Offset = calculateOffset(job.key, interval)
	job.NextRunAt = nextPollAt(time.Now(), interval, job.Offset)
	job.immediateFirstRun = false
}
