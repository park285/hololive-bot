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

package dbx

import (
	"errors"
	"net"
	"testing"
)

func TestIsDNSError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "net.DNSError",
			err:  &net.DNSError{Err: "no such host", Name: "postgres"},
			want: true,
		},
		{
			name: "wrapped net.DNSError",
			err:  errors.New("dial tcp: " + (&net.DNSError{Err: "no such host", Name: "postgres"}).Error()),
			want: true,
		},
		{
			name: "no such host string",
			err:  errors.New("lookup postgres: no such host"),
			want: true,
		},
		{
			name: "connection refused",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "timeout",
			err:  errors.New("timeout"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDNSError(tt.err); got != tt.want {
				t.Errorf("IsDNSError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldFallbackToLocalhost(t *testing.T) {
	tests := []struct {
		name string
		err  error
		host string
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			host: "postgres",
			want: false,
		},
		{
			name: "empty host",
			err:  &net.DNSError{Err: "no such host", Name: "postgres"},
			host: "",
			want: false,
		},
		{
			name: "host is 127.0.0.1",
			err:  &net.DNSError{Err: "no such host", Name: "127.0.0.1"},
			host: "127.0.0.1",
			want: false,
		},
		{
			name: "host is localhost",
			err:  &net.DNSError{Err: "no such host", Name: "localhost"},
			host: "localhost",
			want: false,
		},
		{
			name: "host is not postgres",
			err:  &net.DNSError{Err: "no such host", Name: "db"},
			host: "db",
			want: false,
		},
		{
			name: "postgres DNS error",
			err:  &net.DNSError{Err: "no such host", Name: "postgres"},
			host: "postgres",
			want: true,
		},
		{
			name: "postgres DNS error case insensitive",
			err:  &net.DNSError{Err: "no such host", Name: "POSTGRES"},
			host: "POSTGRES",
			want: true,
		},
		{
			name: "postgres string error",
			err:  errors.New("lookup postgres: no such host"),
			host: "postgres",
			want: true,
		},
		{
			name: "connection refused is not DNS error",
			err:  errors.New("dial tcp postgres:5432: connection refused"),
			host: "postgres",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldFallbackToLocalhost(tt.err, tt.host); got != tt.want {
				t.Errorf("ShouldFallbackToLocalhost() = %v, want %v", got, tt.want)
			}
		})
	}
}
