package botruntime

import (
	"context"
	"errors"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *BotRuntime) closeWebhook(ctx context.Context) error {
	if r.webhookHandlerCloser == nil {
		return nil
	}

	if err := r.webhookHandlerCloser.CloseContext(ctx); err != nil {
		return fmt.Errorf("close context: %w", err)
	}

	return nil
}

func (r *BotRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.cancelBackgroundTasks()
	r.stopHTTPRequestAdmission()

	httpErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger:             r.Logger,
		ShutdownHTTPServer: r.ShutdownHTTPServer,
	})
	usersErr := r.shutdownWebhookAndDurability(ctx)
	webhookErr := r.reportWebhookShutdown(usersErr)
	botErr := r.shutdownBot(ctx)
	shutdownErr := errors.Join(httpErr, webhookErr, botErr)
	joinErr := r.joinBackgroundTasks(ctx)
	requestErr := r.joinHTTPRequests(ctx)

	err := errors.Join(shutdownErr, joinErr, requestErr)
	quiesced := usersErr == nil && requestErr == nil && r.backgroundTasksJoined()

	r.lifecycleMu.Lock()

	r.shutdownErr = err

	if quiesced {
		r.quiesced.Store(true)
	}

	r.lifecycleMu.Unlock()

	return err
}

func (r *BotRuntime) reportWebhookShutdown(err error) error {
	if err != nil {
		r.logError("Iris webhook handler shutdown error", err)

		return fmt.Errorf("close Iris webhook handler: %w", err)
	}

	r.logInfo("Iris webhook handler stopped")

	return nil
}

func (r *BotRuntime) shutdownBot(ctx context.Context) error {
	if r.Bot == nil {
		return nil
	}

	if err := r.Bot.Shutdown(ctx); err != nil {
		r.logError("Error during shutdown", err)

		return fmt.Errorf("shutdown bot: %w", err)
	}

	return nil
}

func (r *BotRuntime) shutdownWebhookAndDurability(ctx context.Context) error {
	closeErr := r.closeWebhook(ctx)

	if r.durable == nil {
		if closeErr != nil {
			return fmt.Errorf("close webhook: %w", closeErr)
		}

		return nil
	}

	return errors.Join(closeErr, r.durable.Stop(ctx))
}
