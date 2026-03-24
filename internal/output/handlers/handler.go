package handlers

import (
	"context"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// OutputWritePlan captures everything needed to persist a single output artifact to the cache.
// Handlers produce write plans during Write() by staging immutable snapshots of the output data.
// The executor later calls Execute() — either inline on the task worker (sync mode) or on a
// dedicated I/O worker (async mode). Cleanup is always called afterward to remove staged data.
type OutputWritePlan interface {
	Execute(ctx context.Context, tracker *worker.ProgressTracker) error
	Cleanup(ctx context.Context) error
}

// PreparedOutput contains the output proto and an optional write plan.
// The output is needed synchronously for hash computation, while the write plan
// can be executed later by the cache writer.
type PreparedOutput struct {
	Output    *gen.Output
	WritePlan OutputWritePlan
}

// Handler defines how to handle a specific type of build output
type Handler interface {
	// Type returns the identifier for this output type (e.g., "dir", "docker")
	Type() HandlerType

	// Write prepares the output and returns the output proto plus an optional write plan.
	Write(ctx context.Context, target model.Target, output model.Output, tracker *worker.ProgressTracker) (*PreparedOutput, error)

	// Hash only hashes the given output without writing it
	// Useful for checking the current local state of the output resource
	Hash(ctx context.Context, target model.Target, output model.Output) (string, error)

	// Load loads the output from the output handler and returns its digest
	Load(ctx context.Context, target model.Target, output *gen.Output, tracker *worker.ProgressTracker) error
}

type HandlerType string

const (
	FileHandler   HandlerType = "file"
	DirHandler    HandlerType = "dir"
	DockerHandler HandlerType = "docker"
)

// KnownHandlerTypes This is necessary so that we can statically check for handler type without having
// to load them during the parsing of the outputs
var KnownHandlerTypes = []HandlerType{
	FileHandler,
	DirHandler,
	DockerHandler,
}
