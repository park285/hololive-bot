package bot

func (b *Bot) ensureCommandExecutor() *CommandRouter {
	if b.commandExecutor == nil {
		b.commandExecutor = NewCommandRouter(b.commandRegistry, b.logger, b.sendMessage)
	}
	return b.commandExecutor
}

func (b *Bot) ensureIngress() *MessageIngress {
	if b.ingress == nil {
		b.ingress = NewMessageIngress(b.messageAdapter, b.acl, b.logger, b.selfSender)
	}
	return b.ingress
}

func (b *Bot) ensureTransport() *CommandTransport {
	if b.transport == nil {
		b.transport = NewCommandTransport(b.irisClient, b.formatter)
	}
	return b.transport
}

func (b *Bot) ensureLifecycle() *BotLifecycle {
	if b.lifecycle == nil {
		b.lifecycle = NewBotLifecycle(
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
