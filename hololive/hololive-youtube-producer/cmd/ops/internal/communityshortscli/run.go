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

func Run(args []string, stdout, stderr io.Writer) int {
	ctx := newCommandContext(stdout, stderr)
	commands := commandRegistry()
	if len(args) == 0 || isUsageRequest(args[0]) {
		if err := writeUsage(ctx.stderr, commands); err != nil {
			return 1
		}
		return 0
	}

	selected, ok := commands[args[0]]
	if !ok {
		return writeUnknownCommand(ctx, commands, args[0])
	}

	return runSelectedCommand(ctx, selected, args[1:])
}

func newCommandContext(stdout, stderr io.Writer) commandContext {
	ctx := commandContext{stdout: stdout, stderr: stderr}
	if ctx.stdout == nil {
		ctx.stdout = os.Stdout
	}
	if ctx.stderr == nil {
		ctx.stderr = os.Stderr
	}
	return ctx
}

func isUsageRequest(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func writeUnknownCommand(ctx commandContext, commands map[string]command, name string) int {
	if _, err := fmt.Fprintf(ctx.stderr, "unknown command %q\n\n", name); err != nil {
		return 1
	}
	if err := writeUsage(ctx.stderr, commands); err != nil {
		return 1
	}
	return 2
}

func runSelectedCommand(ctx commandContext, selected command, args []string) int {
	if err := selected.run(ctx, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if _, writeErr := fmt.Fprintln(ctx.stderr, err.Error()); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func commandRegistry() map[string]command {
	commands := []command{
		{name: "channel-summary", run: runChannelSummaryCommand},
		{name: "delivery-logs", run: runDeliveryLogsCommand},
		{name: "latency-cause-report", run: runLatencyCauseCommand},
		{name: "latency-period-summary", run: runLatencyPeriodSummaryCommand},
		{name: "route-report", run: runRouteReportCommand},
		{name: "send-counts", run: runSendCountsCommand},
		{name: "target-baseline", run: runTargetBaselineCommand},
	}
	indexed := make(map[string]command, len(commands))
	for _, cmd := range commands {
		indexed[cmd.name] = cmd
	}
	return indexed
}

func writeUsage(w io.Writer, commands map[string]command) error {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	if _, err := fmt.Fprintln(w, "usage: youtube-community-shorts <command> [flags]"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "commands:"); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintf(w, "  %s\n", name); err != nil {
			return err
		}
	}
	return nil
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
