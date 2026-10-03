//go:build windows

package greet

import "example.com/mono/internal/winutil"

var _ = winutil.Console
