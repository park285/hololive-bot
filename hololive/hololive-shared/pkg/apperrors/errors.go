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

package errors

import (
	"fmt"
	"strings"
)

type ServiceError struct {
	Service   string
	Operation string
	Message   string
	Err       error
}

func (e *ServiceError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "service error"
	}

	if e.Err == nil {
		return fmt.Sprintf("%s service=%s operation=%s", message, e.Service, e.Operation)
	}

	return fmt.Sprintf("%s service=%s operation=%s: %v", message, e.Service, e.Operation, e.Err)
}

func (e *ServiceError) Unwrap() error { return e.Err }

func NewServiceError(message, service, operation string, cause error) *ServiceError {
	return &ServiceError{
		Service:   service,
		Operation: operation,
		Message:   message,
		Err:       cause,
	}
}
