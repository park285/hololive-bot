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

package orchestration

import (
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/ingress"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/lifecycle"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/orchcmd"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/transport"
)

func (b *Bot) ensureCommandExecutor() *orchcmd.CommandRouter {
	if b.commandExecutor == nil {
		b.commandExecutor = orchcmd.NewCommandRouter(b.commandRegistry, b.logger, b.sendMessage)
	}

	return b.commandExecutor
}

func (b *Bot) ensureIngress() *ingress.MessageIngress {
	if b.ingress == nil {
		b.ingress = ingress.NewMessageIngress(b.messageAdapter, b.acl, b.logger, b.selfSender)
	}

	return b.ingress
}

func (b *Bot) ensureTransport() *transport.CommandTransport {
	if b.transport == nil {
		b.transport = transport.NewCommandTransport(b.irisClient, b.formatter)
	}

	return b.transport
}

func (b *Bot) ensureLifecycle() *lifecycle.BotLifecycle {
	if b.lifecycle == nil {
		b.lifecycle = lifecycle.NewBotLifecycle(
			b.logger,
			b.cache,
			b.irisClient,
			b.irisBaseURL,
			b.stopCh,
			b.doneCh,
			b.workerPool,
			b.holodex,
			b.postgres,
		)
	}

	return b.lifecycle
}
