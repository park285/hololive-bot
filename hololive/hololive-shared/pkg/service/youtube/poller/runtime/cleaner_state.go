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

package polling

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type viewerSampleCleanupCursor struct {
	endedAt time.Time
	videoID string
}

type viewerSampleCleanupState struct {
	cursor      viewerSampleCleanupCursor
	passDeleted int64
}

type viewerSampleCleanupStep struct {
	deleted        int64
	target         *viewerSampleCleanupCursor
	candidateCount int64
	pageEnd        *viewerSampleCleanupCursor
}

func (s viewerSampleCleanupStep) validate() error {
	if s.candidateCount < 0 || s.candidateCount > viewerSampleCleanupSessionPageSize {
		return fmt.Errorf("viewer sample candidate count is out of range: %d", s.candidateCount)
	}

	if (s.candidateCount == 0) != (s.pageEnd == nil) {
		return fmt.Errorf("viewer sample page end does not match candidate count: %d", s.candidateCount)
	}

	if s.target == nil && s.deleted != 0 {
		return fmt.Errorf("viewer sample cleanup deleted %d rows without a target session", s.deleted)
	}

	if s.target != nil && s.deleted <= 0 {
		return fmt.Errorf("viewer sample cleanup target %q produced no deleted rows", s.target.videoID)
	}

	return nil
}

func (s viewerSampleCleanupStep) nextCursor() (viewerSampleCleanupCursor, bool, error) {
	if s.target != nil {
		next := *s.target
		return next, false, nil
	}

	if s.candidateCount < viewerSampleCleanupSessionPageSize {
		return viewerSampleCleanupCursor{}, true, nil
	}

	if s.pageEnd == nil {
		return viewerSampleCleanupCursor{}, false, errors.New("full candidate page has no page-end cursor")
	}

	return *s.pageEnd, false, nil
}

func viewerSampleCleanupCursorFromPG(
	endedAt pgtype.Timestamptz,
	videoID pgtype.Text,
) (viewerSampleCleanupCursor, bool, error) {
	if endedAt.Valid != videoID.Valid {
		return viewerSampleCleanupCursor{}, false, errors.New("cursor fields have mismatched nullability")
	}

	if !endedAt.Valid {
		return viewerSampleCleanupCursor{}, false, nil
	}

	return viewerSampleCleanupCursor{endedAt: endedAt.Time.UTC(), videoID: videoID.String}, true, nil
}

func initialViewerSampleCleanupCursor() viewerSampleCleanupCursor {
	return viewerSampleCleanupCursor{
		endedAt: time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
		videoID: "",
	}
}

func (c *ViewerSampleCleaner) ensureCleanupState() {
	if c.state.cursor.endedAt.IsZero() {
		c.resetCleanupState()
	}
}

func (c *ViewerSampleCleaner) resetCleanupState() {
	c.state = viewerSampleCleanupState{cursor: initialViewerSampleCleanupCursor()}
}
