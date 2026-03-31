package tracing

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

// TraceCollector gathers build metadata and produces a BuildTrace after execution.
type TraceCollector struct {
	traceID           string
	startTime         time.Time
	command           string
	requestedPatterns []label.TargetPattern
	grogVersion       string
}

// NewTraceCollector creates a collector that records metadata at build start.
func NewTraceCollector(
	command string,
	patterns []label.TargetPattern,
	grogVersion string,
) *TraceCollector {
	return &TraceCollector{
		traceID:           uuid.New().String(),
		startTime:         time.Now(),
		command:           command,
		requestedPatterns: patterns,
		grogVersion:       grogVersion,
	}
}

const maxCommandLen = 1024

func truncateCommand(cmd string) string {
	if len(cmd) > maxCommandLen {
		return cmd[:maxCommandLen]
	}
	return cmd
}

// Finalize builds the BuildTrace from the completion map and graph after execution.
func (c *TraceCollector) Finalize(
	completionMap dag.CompletionMap,
	graph *dag.DirectedTargetGraph,
	asyncWaitTime time.Duration,
) *BuildTrace {
	totalDuration := time.Since(c.startTime)

	gitCommit, _ := config.GetGitHash()
	gitBranch, _ := config.GetGitBranch()

	workspace := filepath.Base(config.Global.WorkspaceRoot)
	platform := fmt.Sprintf("%s/%s", config.Global.OS, config.Global.Arch)
	isCI := os.Getenv("CI") == "1"

	var patterns []string
	for _, p := range c.requestedPatterns {
		patterns = append(patterns, p.String())
	}

	trace := &BuildTrace{
		Build: BuildRow{
			TraceID:              c.traceID,
			Workspace:            workspace,
			GitCommit:            gitCommit,
			GitBranch:            gitBranch,
			GrogVersion:          c.grogVersion,
			Platform:             platform,
			Command:              c.command,
			StartTimeUnixMillis:  c.startTime.UnixMilli(),
			TotalDurationMillis:  totalDuration.Milliseconds(),
			RequestedPatterns:    strings.Join(patterns, ","),
			IsCI:                 isCI,
			AsyncCacheWaitMillis: asyncWaitTime.Milliseconds(),
		},
	}

	// Compute critical path if available
	if criticalPath, ok := graph.GetSelectedSubgraph().FindCriticalPath(); ok && len(criticalPath.Nodes) > 0 {
		trace.Build.CriticalPathExecMillis = criticalPath.ExecutionDuration.Milliseconds()
		trace.Build.CriticalPathCacheMillis = criticalPath.CacheDuration.Milliseconds()
	}

	// Build spans from completion map
	nodes := graph.GetNodes()
	for targetLabel, completion := range completionMap {
		if completion.NodeType != model.TargetNode {
			continue
		}

		node, ok := nodes[targetLabel]
		if !ok {
			continue
		}
		target, ok := node.(*model.Target)
		if !ok {
			continue
		}

		span := c.buildSpan(target, &completion)
		trace.Spans = append(trace.Spans, span)

		trace.Build.TotalTargets++
		if completion.IsSuccess {
			trace.Build.SuccessCount++
			if completion.CacheResult == dag.CacheHit {
				trace.Build.CacheHitCount++
			}
		} else {
			trace.Build.FailureCount++
		}
	}

	return trace
}

func (c *TraceCollector) buildSpan(target *model.Target, completion *dag.Completion) SpanRow {
	span := SpanRow{
		TraceID:    c.traceID,
		Label:      target.Label.String(),
		Package:    target.Label.Package,
		ChangeHash: target.ChangeHash,
		OutputHash: target.OutputHash,
		Command:    truncateCommand(target.Command),
		IsTest:     target.IsTest(),
		Tags:       strings.Join(target.Tags, ","),
	}

	// Status
	if completion.IsSuccess {
		span.Status = "SUCCESS"
	} else if completion.Err != nil {
		span.Status = "FAILURE"
	} else {
		span.Status = "CANCELLED"
	}

	// Cache result
	switch completion.CacheResult {
	case dag.CacheHit:
		span.CacheResult = "CACHE_HIT"
	case dag.CacheSkip:
		span.CacheResult = "CACHE_SKIP"
	default:
		span.CacheResult = "CACHE_MISS"
	}

	// Dependencies
	var deps []string
	for _, dep := range target.Dependencies {
		deps = append(deps, dep.String())
	}
	span.Dependencies = strings.Join(deps, ",")

	// Timing
	if !target.StartTime.IsZero() {
		span.StartTimeUnixMillis = target.StartTime.UnixMilli()

		totalDuration := target.QueueWait + target.HashDuration + target.CacheCheckTime +
			target.ExecutionTime + target.OutputWriteTime + target.OutputLoadTime +
			target.CacheWriteTime + target.DepLoadTime
		endTime := target.StartTime.Add(totalDuration)

		span.EndTimeUnixMillis = endTime.UnixMilli()
		span.TotalDurationMillis = totalDuration.Milliseconds()
	}

	span.QueueWaitMillis = target.QueueWait.Milliseconds()
	span.HashDurationMillis = target.HashDuration.Milliseconds()
	span.CacheCheckMillis = target.CacheCheckTime.Milliseconds()
	span.CommandDurationMillis = target.ExecutionTime.Milliseconds()
	span.OutputWriteMillis = target.OutputWriteTime.Milliseconds()
	span.OutputLoadMillis = target.OutputLoadTime.Milliseconds()
	span.CacheWriteMillis = target.CacheWriteTime.Milliseconds()
	span.DepLoadMillis = target.DepLoadTime.Milliseconds()

	return span
}
