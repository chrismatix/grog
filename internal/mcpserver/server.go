package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listTargetsInput struct {
	Patterns   []string `json:"patterns,omitempty" jsonschema:"target patterns to list; defaults to the current package"`
	TargetType string   `json:"target_type,omitempty" jsonschema:"target type: all, test, no_test, or bin_output"`
}

type targetRelationInput struct {
	Target     string `json:"target" jsonschema:"target label to query"`
	Transitive bool   `json:"transitive,omitempty" jsonschema:"include the complete transitive relation"`
	TargetType string `json:"target_type,omitempty" jsonschema:"target type: all, test, no_test, or bin_output"`
}

type ownersInput struct {
	Files []string `json:"files" jsonschema:"workspace-relative or absolute file paths"`
}

type changedTargetsInput struct {
	Since             string `json:"since" jsonschema:"Git ref or Jujutsu revision to compare against"`
	IncludeDependents bool   `json:"include_dependents,omitempty" jsonschema:"include transitive dependents of changed targets"`
	TargetType        string `json:"target_type,omitempty" jsonschema:"target type: all, test, no_test, or bin_output"`
}

type explainChangesInput struct {
	Since      string `json:"since" jsonschema:"Git ref or Jujutsu revision to compare against"`
	ShowFiles  *bool  `json:"show_files,omitempty" jsonschema:"include changed files in the explanation"`
	FilesFirst bool   `json:"files_first,omitempty" jsonschema:"root the explanation at changed files"`
}

type dependencyGraphInput struct {
	Patterns   []string `json:"patterns,omitempty" jsonschema:"target patterns to include; defaults to the whole workspace"`
	Transitive bool     `json:"transitive,omitempty" jsonschema:"include transitive dependencies"`
}

type targetsOutput struct {
	Targets []string `json:"targets"`
}

type explanationOutput struct {
	Explanation string `json:"explanation"`
}

type dependencyGraphOutput struct {
	Graph any `json:"graph"`
}

// Run starts the Grog MCP server on stdin and stdout.
func Run(contextValue context.Context, version string) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate Grog executable: %w", err)
	}
	server := newServer(version, commandRunner{executablePath: executablePath})
	return server.Run(contextValue, &mcp.StdioTransport{})
}

func newServer(version string, runner queryRunner) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "grog", Version: version}, nil)
	addTargetTools(server, runner)
	addChangeTools(server, runner)
	addGraphTool(server, runner)
	return server
}

func addTargetTools(server *mcp.Server, runner queryRunner) {
	mcp.AddTool(server, readOnlyTool("list_targets", "List Grog targets matching optional patterns and target type."), func(contextValue context.Context, _ *mcp.CallToolRequest, input listTargetsInput) (*mcp.CallToolResult, targetsOutput, error) {
		arguments := append([]string{"list"}, input.Patterns...)
		arguments = appendTargetType(arguments, input.TargetType)
		output, err := runTargetsQuery(contextValue, runner, arguments...)
		return nil, output, err
	})

	mcp.AddTool(server, readOnlyTool("get_dependencies", "List direct or transitive dependencies of a Grog target."), func(contextValue context.Context, _ *mcp.CallToolRequest, input targetRelationInput) (*mcp.CallToolResult, targetsOutput, error) {
		arguments := []string{"deps", input.Target}
		arguments = appendRelationOptions(arguments, input)
		output, err := runTargetsQuery(contextValue, runner, arguments...)
		return nil, output, err
	})

	mcp.AddTool(server, readOnlyTool("get_dependents", "List direct or transitive dependents of a Grog target."), func(contextValue context.Context, _ *mcp.CallToolRequest, input targetRelationInput) (*mcp.CallToolResult, targetsOutput, error) {
		arguments := []string{"rdeps", input.Target}
		arguments = appendRelationOptions(arguments, input)
		output, err := runTargetsQuery(contextValue, runner, arguments...)
		return nil, output, err
	})

	mcp.AddTool(server, readOnlyTool("find_owners", "Find Grog targets that own any of the provided files."), func(contextValue context.Context, _ *mcp.CallToolRequest, input ownersInput) (*mcp.CallToolResult, targetsOutput, error) {
		arguments := append([]string{"owners"}, input.Files...)
		output, err := runTargetsQuery(contextValue, runner, arguments...)
		return nil, output, err
	})
}

func addChangeTools(server *mcp.Server, runner queryRunner) {
	mcp.AddTool(server, readOnlyTool("get_changed_targets", "List targets affected by changes since a Git or Jujutsu revision."), func(contextValue context.Context, _ *mcp.CallToolRequest, input changedTargetsInput) (*mcp.CallToolResult, targetsOutput, error) {
		arguments := []string{"changes", "--since=" + input.Since}
		if input.IncludeDependents {
			arguments = append(arguments, "--dependents=transitive")
		}
		arguments = appendTargetType(arguments, input.TargetType)
		output, err := runTargetsQuery(contextValue, runner, arguments...)
		return nil, output, err
	})

	mcp.AddTool(server, readOnlyTool("explain_changes", "Explain changed inputs and their affected target paths."), func(contextValue context.Context, _ *mcp.CallToolRequest, input explainChangesInput) (*mcp.CallToolResult, explanationOutput, error) {
		arguments := []string{"explain-changes", "--since=" + input.Since}
		if input.ShowFiles != nil {
			arguments = append(arguments, fmt.Sprintf("--show-files=%t", *input.ShowFiles))
		}
		if input.FilesFirst {
			arguments = append(arguments, "--files-first")
		}
		output, err := runExplanationQuery(contextValue, runner, arguments...)
		return nil, output, err
	})
}

func addGraphTool(server *mcp.Server, runner queryRunner) {
	mcp.AddTool(server, readOnlyTool("get_dependency_graph", "Return the Grog dependency graph for optional target patterns."), func(contextValue context.Context, _ *mcp.CallToolRequest, input dependencyGraphInput) (*mcp.CallToolResult, dependencyGraphOutput, error) {
		arguments := append([]string{"graph"}, input.Patterns...)
		arguments = append(arguments, "--output=json")
		if input.Transitive {
			arguments = append(arguments, "--transitive")
		}
		output, err := runGraphQuery(contextValue, runner, arguments...)
		return nil, output, err
	})
}

func readOnlyTool(name string, description string) *mcp.Tool {
	closedWorld := false
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: &closedWorld,
		},
	}
}

func appendRelationOptions(arguments []string, input targetRelationInput) []string {
	if input.Transitive {
		arguments = append(arguments, "--transitive")
	}
	return appendTargetType(arguments, input.TargetType)
}

func appendTargetType(arguments []string, targetType string) []string {
	if targetType != "" {
		arguments = append(arguments, "--target-type="+targetType)
	}
	return arguments
}

func runTargetsQuery(contextValue context.Context, runner queryRunner, arguments ...string) (targetsOutput, error) {
	results, err := runner.run(contextValue, arguments...)
	if err != nil {
		return targetsOutput{}, err
	}

	output := targetsOutput{Targets: []string{}}
	for _, result := range results {
		var resultOutput targetsOutput
		if err := json.Unmarshal(result, &resultOutput); err != nil {
			return targetsOutput{}, fmt.Errorf("could not decode targets: %w", err)
		}
		output.Targets = append(output.Targets, resultOutput.Targets...)
	}
	return output, nil
}

func runExplanationQuery(contextValue context.Context, runner queryRunner, arguments ...string) (explanationOutput, error) {
	results, err := runner.run(contextValue, arguments...)
	if err != nil {
		return explanationOutput{}, err
	}

	var explanations []string
	for _, result := range results {
		var textOutput struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(result, &textOutput); err != nil {
			return explanationOutput{}, fmt.Errorf("could not decode explanation: %w", err)
		}
		explanations = append(explanations, textOutput.Text)
	}
	return explanationOutput{Explanation: strings.Join(explanations, "")}, nil
}

func runGraphQuery(contextValue context.Context, runner queryRunner, arguments ...string) (dependencyGraphOutput, error) {
	results, err := runner.run(contextValue, arguments...)
	if err != nil {
		return dependencyGraphOutput{}, err
	}
	if len(results) == 0 {
		return dependencyGraphOutput{}, nil
	}

	var output dependencyGraphOutput
	if err := json.Unmarshal(results[0], &output); err != nil {
		return dependencyGraphOutput{}, fmt.Errorf("could not decode dependency graph: %w", err)
	}
	return output, nil
}
