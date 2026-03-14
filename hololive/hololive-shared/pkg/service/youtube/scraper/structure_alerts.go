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

package scraper

import "log/slog"

func logStructureWarning(surface, channelID, detail string, attrs ...any) {
	baseAttrs := make([]any, 0, 6+len(attrs))
	baseAttrs = append(baseAttrs,
		"surface", surface,
		"channel_id", channelID,
		"detail", detail,
	)
	baseAttrs = append(baseAttrs, attrs...)
	slog.Warn("YouTube scraper structure signal", baseAttrs...)
}

func looksEmptyChannelStats(stats *ChannelStats) bool {
	if stats == nil {
		return true
	}
	return stats.SubscriberCount == 0 &&
		stats.ViewCount == 0 &&
		stats.VideoCount == 0 &&
		stats.JoinedDate == 0 &&
		stats.Description == "" &&
		stats.Country == "" &&
		stats.Handle == ""
}
