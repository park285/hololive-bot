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

package auth

import "time"

type Config struct {
	SessionTTL    time.Duration
	ResetTokenTTL time.Duration

	LoginRateLimitPerMinute                int64
	PasswordResetRequestRateLimitPerMinute int64

	LoginFailLimit    int64
	LoginFailWindow   time.Duration
	LoginLockDuration time.Duration

	UserSessionsTTL time.Duration

	AutoPrepareSchema bool
}

func DefaultConfig() Config {
	sessionTTL := 7 * 24 * time.Hour
	return Config{
		SessionTTL:                             sessionTTL,
		ResetTokenTTL:                          60 * time.Minute,
		LoginRateLimitPerMinute:                30,
		PasswordResetRequestRateLimitPerMinute: 10,
		LoginFailLimit:                         5,
		LoginFailWindow:                        15 * time.Minute,
		LoginLockDuration:                      15 * time.Minute,
		UserSessionsTTL:                        sessionTTL + (24 * time.Hour),
		AutoPrepareSchema:                      true,
	}
}
