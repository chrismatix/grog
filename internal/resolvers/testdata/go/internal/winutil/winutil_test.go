package winutil

import (
	_ "embed"
	"testing"

	"example.com/mono/internal/greet"
)

//go:embed all:fixtures
var fixtures string

/*
//go:embed [
*/
func TestConsole(t *testing.T) { _ = greet.Hello; _ = fixtures }
