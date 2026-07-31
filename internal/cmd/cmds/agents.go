package cmds

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/loading"
	"grog/internal/model"

	"github.com/spf13/cobra"
)

const (
	agentsFragmentStart = "<!-- grog:agents:start -->"
	agentsFragmentEnd   = "<!-- grog:agents:end -->"
)

var agentsInitOptions = struct {
	filePath string
	stdout   bool
}{
	filePath: "AGENTS.md",
}

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Generates repository guidance for coding agents.",
}

var agentsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Adds Grog commands and targets to an agent instruction file.",
	Args:  cobra.NoArgs,
	Run: func(command *cobra.Command, arguments []string) {
		commandContext, logger := console.SetupCommand()
		graph := loading.MustLoadGraphForQuery(commandContext, logger)
		fragment := renderAgentsFragment(graph)

		if agentsInitOptions.stdout {
			fmt.Print(fragment)
			return
		}

		outputPath := agentsInitOptions.filePath
		if !filepath.IsAbs(outputPath) {
			outputPath = filepath.Join(config.Global.WorkspaceRoot, outputPath)
		}
		if err := writeAgentsFragment(outputPath, fragment); err != nil {
			logger.Fatalf("could not write agent instructions: %v", err)
		}
		logger.Infof("Updated %s", outputPath)
	},
}

func renderAgentsFragment(graph *dag.DirectedTargetGraph) string {
	var buildTargets []string
	var testTargets []string
	var binaryTargets []string

	for _, node := range graph.GetNodes() {
		target, ok := node.(*model.Target)
		if !ok {
			continue
		}

		label := target.Label.String()
		switch {
		case target.IsTestOnly():
			testTargets = append(testTargets, label)
		case target.BinOutput.IsSet():
			binaryTargets = append(binaryTargets, label)
		default:
			buildTargets = append(buildTargets, label)
		}
	}

	sort.Strings(buildTargets)
	sort.Strings(testTargets)
	sort.Strings(binaryTargets)

	var buffer bytes.Buffer
	fmt.Fprintln(&buffer, agentsFragmentStart)
	fmt.Fprintln(&buffer, "## Grog")
	fmt.Fprintln(&buffer)
	fmt.Fprintln(&buffer, "Use Grog as the repository's canonical build and test interface:")
	fmt.Fprintln(&buffer)
	fmt.Fprintln(&buffer, "- Validate configuration with `grog check`.")
	fmt.Fprintln(&buffer, "- Build the workspace with `grog build //...`.")
	fmt.Fprintln(&buffer, "- Test the workspace with `grog test //...`.")
	fmt.Fprintln(&buffer, "- Inspect affected targets with `grog changes --since=HEAD`.")
	fmt.Fprintln(&buffer, "- Run a narrower target by passing its label to `grog build` or `grog test`.")

	writeAgentTargetGroup(&buffer, "Build targets", buildTargets)
	writeAgentTargetGroup(&buffer, "Test targets", testTargets)
	writeAgentTargetGroup(&buffer, "Binary targets", binaryTargets)

	fmt.Fprintln(&buffer, agentsFragmentEnd)
	return buffer.String()
}

func writeAgentTargetGroup(buffer *bytes.Buffer, heading string, labels []string) {
	if len(labels) == 0 {
		return
	}

	fmt.Fprintf(buffer, "\n### %s\n\n", heading)
	for _, label := range labels {
		fmt.Fprintf(buffer, "- `%s`\n", label)
	}
}

func writeAgentsFragment(path string, fragment string) error {
	existingContent, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	updatedContent, err := replaceAgentsFragment(string(existingContent), fragment)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if fileInfo, statError := os.Stat(path); statError == nil {
		mode = fileInfo.Mode().Perm()
	}
	return os.WriteFile(path, []byte(updatedContent), mode)
}

func replaceAgentsFragment(existingContent string, fragment string) (string, error) {
	startIndex := strings.Index(existingContent, agentsFragmentStart)
	endIndex := strings.Index(existingContent, agentsFragmentEnd)
	if (startIndex >= 0) != (endIndex >= 0) {
		return "", fmt.Errorf("found an incomplete Grog agent fragment")
	}
	if endIndex >= 0 && endIndex < startIndex {
		return "", fmt.Errorf("found an invalid Grog agent fragment")
	}

	if startIndex >= 0 {
		endIndex += len(agentsFragmentEnd)
		return existingContent[:startIndex] + strings.TrimSuffix(fragment, "\n") + existingContent[endIndex:], nil
	}

	if existingContent == "" {
		return fragment, nil
	}
	return strings.TrimRight(existingContent, "\n") + "\n\n" + fragment, nil
}

// AddAgentsCmd registers commands that generate coding-agent instructions.
func AddAgentsCmd(rootCmd *cobra.Command) {
	agentsInitCmd.Flags().StringVar(&agentsInitOptions.filePath, "file", "AGENTS.md", "Instruction file path, relative to the workspace root")
	agentsInitCmd.Flags().BoolVar(&agentsInitOptions.stdout, "stdout", false, "Print the managed fragment instead of writing a file")
	agentsCmd.AddCommand(agentsInitCmd)
	rootCmd.AddCommand(agentsCmd)
}
