package cmds

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"grog/internal/dag"
	"grog/internal/execution"
	"grog/internal/label"
	"grog/internal/model"
)

type agentFailureGroup struct {
	labels   []string
	exitCode *int
	command  string
	output   []string
	message  string
}

func renderAgentFailures(
	writer io.Writer,
	completionMap dag.CompletionMap,
	graph *dag.DirectedTargetGraph,
	maximumLogLines int,
	maximumFailures int,
) {
	if maximumLogLines < 0 {
		maximumLogLines = 0
	}
	if maximumFailures < 1 {
		maximumFailures = 1
	}

	var failedLabels []string
	for completionLabel, completion := range completionMap {
		if !completion.IsSuccess {
			failedLabels = append(failedLabels, completionLabel.String())
		}
	}
	sort.Strings(failedLabels)

	groupsBySignature := make(map[string]*agentFailureGroup)
	var orderedGroups []*agentFailureGroup
	includedFailures := 0
	for _, failedLabel := range failedLabels {
		if includedFailures >= maximumFailures {
			break
		}

		targetLabel, err := label.ParseTargetLabel("", failedLabel)
		if err != nil {
			continue
		}
		completion := completionMap[targetLabel]
		target, ok := graph.GetNodes()[targetLabel].(*model.Target)
		if !ok {
			continue
		}

		group, signature := newAgentFailureGroup(target, completion.Err, maximumLogLines)
		existingGroup, exists := groupsBySignature[signature]
		if exists {
			existingGroup.labels = append(existingGroup.labels, failedLabel)
		} else {
			group.labels = []string{failedLabel}
			groupsBySignature[signature] = group
			orderedGroups = append(orderedGroups, group)
		}
		includedFailures++
	}

	for groupIndex, group := range orderedGroups {
		if groupIndex > 0 {
			fmt.Fprintln(writer)
		}
		fmt.Fprintf(writer, "FAIL %s", strings.Join(group.labels, ", "))
		if group.exitCode != nil {
			fmt.Fprintf(writer, " (exit %d)", *group.exitCode)
		}
		fmt.Fprintln(writer)
		if group.command != "" {
			fmt.Fprintf(writer, "command: %s\n", group.command)
		}
		if group.message != "" {
			fmt.Fprintf(writer, "error: %s\n", group.message)
		}
		if len(group.output) > 0 {
			fmt.Fprintln(writer, "output:")
			for _, line := range group.output {
				fmt.Fprintln(writer, line)
			}
		}
	}

	if omittedFailures := len(failedLabels) - includedFailures; omittedFailures > 0 {
		fmt.Fprintf(writer, "\n... %d more failing targets omitted\n", omittedFailures)
	}
}

func newAgentFailureGroup(target *model.Target, failure error, maximumLogLines int) (*agentFailureGroup, string) {
	group := &agentFailureGroup{command: target.Command}

	var commandError *execution.CommandError
	if errors.As(failure, &commandError) {
		exitCode := commandError.ExitCode
		group.exitCode = &exitCode
		group.output = tailDeduplicatedLines(commandError.Output, maximumLogLines)
		signature := strings.Join([]string{
			"command",
			strconv.Itoa(exitCode),
			target.Command,
			strings.Join(group.output, "\n"),
		}, "\x00")
		return group, signature
	}

	if failure != nil {
		group.message = failure.Error()
	}
	return group, "error\x00" + target.Command + "\x00" + group.message
}

func tailDeduplicatedLines(output string, maximumLines int) []string {
	if maximumLines == 0 {
		return nil
	}

	normalizedOutput := strings.ReplaceAll(output, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(normalizedOutput), "\n")
	if len(lines) > maximumLines {
		lines = lines[len(lines)-maximumLines:]
	}

	seen := make(map[string]bool)
	deduplicated := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		deduplicated = append(deduplicated, line)
	}
	return deduplicated
}
