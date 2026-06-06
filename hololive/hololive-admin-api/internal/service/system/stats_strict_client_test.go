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

package system

import (
	"path/filepath"
	"testing"
)

func TestClientForURLReturnsNotOKWhenH3ClientUnavailable(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

	collector := NewCollector(nil)

	client, ok := collector.clientForURL("https://hololive-bot:30191/health")
	if ok || client != nil {
		t.Fatalf("clientForURL(https) = %v, %v; want nil client and ok=false when h3 config fails", client, ok)
	}
}

func TestClientForURLKeepsPlainHTTPClientWhenH3Unavailable(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

	collector := NewCollector(nil)

	client, ok := collector.clientForURL("http://hololive-bot:30191/health")
	if !ok || client == nil {
		t.Fatalf("clientForURL(http) = %v, %v; want usable tcp client", client, ok)
	}
}
