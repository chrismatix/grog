package handlers

import (
	"context"
	"grog/internal/model"
	"grog/internal/proto/gen"
)

// Handler defines how to handle a specific type of build output
type Handler interface {
	// Type returns the identifier for this output type (e.g., "dir", "docker")
	Type() HandlerType

	// Write writes the output to the output handler and returns its digest
	Write(ctx context.Context, target model.Target, output model.Output) (*gen.Output, error)

	// Hash only hashes the given output without writing it
	// Useful for checking the current local state of the output resource
	Hash(ctx context.Context, target model.Target, output model.Output) (string, error)

	// Load loads the output from the output handler and returns its digest
	Load(ctx context.Context, target model.Target, output *gen.Output) error
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
