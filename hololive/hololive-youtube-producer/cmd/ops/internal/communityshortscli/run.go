package communityshortscli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type commandContext struct {
	stdout io.Writer
	stderr io.Writer
}

type command struct {
	name string
	run  func(commandContext, []string) error
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	ctx := newCommandContext(stdout, stderr)
	commands := commandRegistry()
	if isUsageRequest(args) {
		writeUsage(ctx.stderr, commands)
		return 0
	}

	selected, ok := commands[args[0]]
	if !ok {
		return writeUnknownCommand(ctx, commands, args[0])
	}

	return runSelectedCommand(ctx, selected, args[1:])
}

func newCommandContext(stdout io.Writer, stderr io.Writer) commandContext {
	ctx := commandContext{stdout: stdout, stderr: stderr}
	if ctx.stdout == nil {
		ctx.stdout = os.Stdout
	}
	if ctx.stderr == nil {
		ctx.stderr = os.Stderr
	}
	return ctx
}

func isUsageRequest(args []string) bool {
	return len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help"
}

func writeUnknownCommand(ctx commandContext, commands map[string]command, name string) int {
	fmt.Fprintf(ctx.stderr, "unknown command %q\n\n", name)
	writeUsage(ctx.stderr, commands)
	return 2
}

func runSelectedCommand(ctx commandContext, selected command, args []string) int {
	if err := selected.run(ctx, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(ctx.stderr, err.Error())
		return 1
	}
	return 0
}

func commandRegistry() map[string]command {
	commands := []command{
		{name: "alarm-sent-history-dataset", run: runAlarmSentHistoryDatasetCommand},
		{name: "channel-summary", run: runChannelSummaryCommand},
		{name: "community-alarm-sent-history", run: runCommunityAlarmSentHistoryCommand},
		{name: "continuous-observation-report", run: runContinuousObservationCommand},
		{name: "delivery-logs", run: runDeliveryLogsCommand},
		{name: "latency-cause-report", run: runLatencyCauseCommand},
		{name: "latency-period-summary", run: runLatencyPeriodSummaryCommand},
		{name: "route-report", run: runRouteReportCommand},
		{name: "send-counts", run: runSendCountsCommand},
		{name: "send-state", run: runSendStateCommand},
		{name: "shorts-alarm-sent-history", run: runShortsAlarmSentHistoryCommand},
		{name: "target-baseline", run: runTargetBaselineCommand},
	}
	indexed := make(map[string]command, len(commands))
	for _, cmd := range commands {
		indexed[cmd.name] = cmd
	}
	return indexed
}

func writeUsage(w io.Writer, commands map[string]command) {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(w, "usage: youtube-community-shorts <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	for _, name := range names {
		fmt.Fprintf(w, "  %s\n", name)
	}
}

func newFlagSet(ctx commandContext, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ctx.stderr)
	return fs
}

type periodFlagValues []string

func (p *periodFlagValues) String() string {
	return strings.Join(*p, ",")
}

func (p *periodFlagValues) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("period value is empty")
	}
	*p = append(*p, trimmed)
	return nil
}
