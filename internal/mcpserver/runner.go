package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type queryRunner interface {
	run(context.Context, ...string) ([]json.RawMessage, error)
}

type commandRunner struct {
	executablePath string
}

type outputEnvelope struct {
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (runner commandRunner) run(contextValue context.Context, arguments ...string) ([]json.RawMessage, error) {
	commandArguments := append(append([]string{}, arguments...), "--json", "--log-level=error")
	command := exec.CommandContext(contextValue, runner.executablePath, commandArguments...)

	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError

	commandError := command.Run()
	results, outputError := parseOutput(standardOutput.Bytes())
	if outputError != nil {
		return nil, outputError
	}
	if commandError == nil {
		return results, nil
	}

	errorMessage := strings.TrimSpace(standardError.String())
	if errorMessage == "" {
		errorMessage = commandError.Error()
	}
	return nil, errors.New(errorMessage)
}

func parseOutput(output []byte) ([]json.RawMessage, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	var results []json.RawMessage
	for scanner.Scan() {
		var envelope outputEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return nil, fmt.Errorf("could not decode Grog JSON output: %w", err)
		}
		switch envelope.Type {
		case "result":
			results = append(results, envelope.Data)
		case "error":
			if envelope.Error == nil {
				return nil, errors.New("Grog query failed")
			}
			return nil, fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not read Grog JSON output: %w", err)
	}
	return results, nil
}
