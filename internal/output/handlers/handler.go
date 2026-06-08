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

// Handler defines how to handle a specific type of build output.
type Handler interface {
	// Type returns the identifier for this output type (e.g., "dir", "oci")
	Type() HandlerType

	// Write prepares the output and returns the output proto plus an optional write plan.
	Write(ctx context.Context, target model.Target, output model.Output, tracker *worker.ProgressTracker) (*PreparedOutput, error)

	// Hash only hashes the given output without writing it
	// Useful for checking the current local state of the output resource
	Hash(ctx context.Context, target model.Target, output model.Output) (string, error)

	// Load loads the output from the output handler and returns its digest
	Load(ctx context.Context, target model.Target, output *gen.Output, tracker *worker.ProgressTracker) error
}

// ImagePusher is the optional capability oci output handlers implement to
// ship a cached image to a user-facing registry. The handler reads its own
// cache state to resolve a source ref and copies it daemon-free to the
// destination. Driven by target.OciPush entries after Write/Load.
type ImagePusher interface {
	PushImage(ctx context.Context, image *gen.OCIImageOutput, destination string, tracker *worker.ProgressTracker) (skipped bool, err error)
}

type HandlerType string

const (
	FileHandler HandlerType = "file"
	DirHandler  HandlerType = "dir"
	OCIHandler  HandlerType = "oci"
)

// KnownHandlerTypes This is necessary so that we can statically check for handler type without having
// to load them during the parsing of the outputs.
var KnownHandlerTypes = []HandlerType{
	FileHandler,
	DirHandler,
	OCIHandler,
}
