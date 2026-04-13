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

package constants

import "time"

const (
	Tier1Window = 45 * time.Minute
	Tier2Window = 3 * time.Hour
	Tier3Window = 12 * time.Hour
)

const (
	Tier1Interval = 1 * time.Minute
	Tier2Interval = 3 * time.Minute
	Tier3Interval = 10 * time.Minute
	Tier4Interval = 15 * time.Minute
)

const (
	NoUpcomingInterval        = 1 * time.Minute
	FullRefreshInterval       = 5 * time.Minute
	RecentlyNotifiedWindow    = 15 * time.Minute
	LiveCatchupSuppressWindow = 15 * time.Minute
)

const (
	LocalFallbackDedupTTL       = 10 * time.Minute
	LocalFallbackCleanupMaxKeys = 4096
)

var DefaultTargetMinutes = []int{5, 3, 1}
