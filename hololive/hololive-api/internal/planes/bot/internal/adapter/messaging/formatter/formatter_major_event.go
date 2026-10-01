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

package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type majorEventSubscribedData struct {
	Prefix string
}

type majorEventStatusData struct {
	IsSubscribed bool
	Prefix       string
}

type majorEventUsageData struct {
	Prefix string
}

func (f *ResponseFormatter) FormatMajorEventSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventSubscribed, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUnsubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUnsubscribed, majorEventSubscribedData{})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventAlreadySubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventAlreadySub, majorEventSubscribedData{})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventNotSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{Prefix: f.prefix}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventNotSub, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventStatus(ctx context.Context, isSubscribed bool) string {
	data := majorEventStatusData{
		IsSubscribed: isSubscribed,
		Prefix:       f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventStatus, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUsage(ctx context.Context) string {
	data := majorEventUsageData{
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUsage, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
