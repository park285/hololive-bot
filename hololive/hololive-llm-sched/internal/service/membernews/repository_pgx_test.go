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

package membernews

import (
	"errors"
	"strings"
	"testing"
)

func TestNewPGXMemberNewsQuerier_NilPool(t *testing.T) {
	if got := newPGXMemberNewsQuerier(nil); got != nil {
		t.Fatalf("expected nil querier for nil pool")
	}
}

func TestNilRowScanner_Scan(t *testing.T) {
	if err := (nilRowScanner{}).Scan(); err == nil || !strings.Contains(err.Error(), "row scanner is nil") {
		t.Fatalf("expected default nilRowScanner error, got %v", err)
	}

	injectedErr := errors.New("boom")
	if err := (nilRowScanner{err: injectedErr}).Scan(); !errors.Is(err, injectedErr) {
		t.Fatalf("expected injected error, got %v", err)
	}
}
