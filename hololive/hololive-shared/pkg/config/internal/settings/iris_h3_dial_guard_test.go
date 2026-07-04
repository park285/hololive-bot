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

package settings

import (
	"net"
	"testing"
	"time"
)

func TestSettingsIrisH3DialGuardAllowsOnlyBaseURLLiteralIP(t *testing.T) {
	t.Parallel()

	guard := newSettingsIrisH3DialGuard("https://192.0.2.10:3001", time.Second)
	if err := guard(net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("guard(allowed ip) error = %v", err)
	}
	if err := guard(net.ParseIP("192.0.2.11")); err == nil {
		t.Fatal("guard(disallowed ip) error = nil, want denial")
	}
	if err := guard(nil); err == nil {
		t.Fatal("guard(nil) error = nil, want denial")
	}
}
