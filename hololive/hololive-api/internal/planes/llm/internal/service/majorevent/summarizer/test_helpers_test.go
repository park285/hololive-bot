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

package summarizer

import (
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
)

const (
	severityCritical = "critical"
	severityWarning  = "warning"
	severityInfo     = "info"
)

const (
	testFesName    = "fes"
	testEventNameA = "Event A"
	testEventDateA = "3/1(토)"
	testEventNote  = "테스트"
	testEventTitle = "Test"

	testLinkFes = "https://example.com/fes"
	testLinkOne = "https://example.com/1"
)

const (
	wantKeyType                 = "type"
	wantKeyDescription          = "description"
	wantKeyProperties           = "properties"
	wantKeyRequired             = "required"
	wantKeyAdditionalProperties = "additionalProperties"
	wantKeyMaxLength            = "maxLength"

	wantTypeObject = "object"
	wantTypeString = "string"
	wantTypeArray  = "array"

	wantFieldName    = "name"
	wantFieldDate    = "date"
	wantFieldMembers = "members"
	wantFieldNote    = "note"
	wantFieldLink    = "link"
	wantFieldSource  = "source"
)

var testLogger = sharedlogging.NewLogger
