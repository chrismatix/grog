package handlers

import (
	"context"
	"grog/internal/model"
)

// Handler defines how to handle a specific type of build output
type Handler interface {
	// Type returns the identifier for this output type (e.g., "dir", "docker")
	Type() HandlerType

	Has(ctx context.Context, target model.Target, output model.Output) (bool, error)

	Write(ctx context.Context, target model.Target, output model.Output) (string, error)

	Load(ctx context.Context, target model.Target, output model.Output) (string, error)
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
