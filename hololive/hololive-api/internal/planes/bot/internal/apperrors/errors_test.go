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

package apperrors

import (
	"errors"
	"strings"
	"testing"
)

func TestServiceError_ErrorAndFactory(t *testing.T) {
	base := ServiceError{Service: "member", Operation: "sync"}
	if got := base.Error(); got != "service error service=member operation=sync" {
		t.Fatalf("unexpected ServiceError string: %q", got)
	}

	cause := errors.New("db failed")

	withCause := ServiceError{Service: "member", Operation: "sync", Err: cause}
	if got := withCause.Error(); !strings.Contains(got, "db failed") {
		t.Fatalf("expected cause in ServiceError string: %q", got)
	}

	if !errors.Is(withCause.Unwrap(), cause) {
		t.Fatal("ServiceError.Unwrap() mismatch")
	}

	created := NewServiceError("ignored", "alarm", "publish", cause)
	if created.Service != "alarm" || created.Operation != "publish" || !errors.Is(created.Err, cause) {
		t.Fatalf("unexpected NewServiceError result: %+v", created)
	}
}
