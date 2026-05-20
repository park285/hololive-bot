package communityshortscli

import (
	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
)

func runCommunityAlarmSentHistoryCommand(ctx commandContext, args []string) error {
	return runRequiredObservationReport(
		ctx,
		args,
		"community-alarm-sent-history",
		reportcli.RequiredObservationCommand[
			opsapp.CommunityAlarmSentHistoryCollectOptions,
			opsapp.CommunityAlarmSentHistoryReport,
		]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.CommunityAlarmSentHistoryCollectOptions {
				return opsapp.CommunityAlarmSentHistoryCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectCommunityAlarmSentHistoryReport,
			RenderMarkdown:     opsapp.RenderCommunityAlarmSentHistoryMarkdown,
			LoadConfigError:    "Failed to load community alarm sent-history config",
			CollectError:       "Failed to collect community alarm sent history",
			MarkdownWriteError: "Failed to write community alarm sent-history markdown",
			JSONWriteError:     "Failed to write community alarm sent-history JSON",
		},
	)
}

func runShortsAlarmSentHistoryCommand(ctx commandContext, args []string) error {
	return runRequiredObservationReport(
		ctx,
		args,
		"shorts-alarm-sent-history",
		reportcli.RequiredObservationCommand[
			opsapp.ShortsAlarmSentHistoryCollectOptions,
			opsapp.ShortsAlarmSentHistoryReport,
		]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.ShortsAlarmSentHistoryCollectOptions {
				return opsapp.ShortsAlarmSentHistoryCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectShortsAlarmSentHistoryReport,
			RenderMarkdown:     opsapp.RenderShortsAlarmSentHistoryMarkdown,
			LoadConfigError:    "Failed to load shorts alarm sent-history config",
			CollectError:       "Failed to collect shorts alarm sent history",
			MarkdownWriteError: "Failed to write shorts alarm sent-history markdown",
			JSONWriteError:     "Failed to write shorts alarm sent-history JSON",
		},
	)
}

func runAlarmSentHistoryDatasetCommand(ctx commandContext, args []string) error {
	return runRequiredObservationReport(
		ctx,
		args,
		"alarm-sent-history-dataset",
		reportcli.RequiredObservationCommand[
			opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions,
			opsapp.CommunityShortsAlarmSentHistoryDatasetReport,
		]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions {
				return opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectCommunityShortsAlarmSentHistoryDatasetReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsAlarmSentHistoryDatasetMarkdown,
			LoadConfigError:    "Failed to load community/shorts alarm sent-history dataset config",
			CollectError:       "Failed to collect community/shorts alarm sent history dataset",
			MarkdownWriteError: "Failed to write community/shorts alarm sent-history dataset markdown",
			JSONWriteError:     "Failed to write community/shorts alarm sent-history dataset JSON",
		},
	)
}

func runSendStateCommand(ctx commandContext, args []string) error {
	return runRequiredObservationReport(
		ctx,
		args,
		"send-state",
		reportcli.RequiredObservationCommand[
			opsapp.CommunityShortsSendStateCollectOptions,
			opsapp.CommunityShortsSendStateReport,
		]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.CommunityShortsSendStateCollectOptions {
				return opsapp.CommunityShortsSendStateCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectCommunityShortsSendStateReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsSendStateMarkdown,
			LoadConfigError:    "Failed to load community/shorts send-state config",
			CollectError:       "Failed to collect community/shorts send state report",
			MarkdownWriteError: "Failed to write community/shorts send-state markdown",
			JSONWriteError:     "Failed to write community/shorts send-state JSON",
		},
	)
}

func runRequiredObservationReport[Options any, Report any](
	ctx commandContext,
	args []string,
	name string,
	command reportcli.RequiredObservationCommand[Options, Report],
) error {
	fs := newFlagSet(ctx, name)
	observationRuntime := fs.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := fs.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := fs.String("format", "markdown", "output format: markdown or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	command.Stdout = ctx.stdout
	command.Stderr = ctx.stderr
	return reportcli.RunRequiredObservationReport(
		reportcli.RequiredObservationParams{
			Runtime: *observationRuntime,
			Cutover: *observationCutover,
			Format:  *format,
		},
		command,
	)
}
