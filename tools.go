//go:build tools
// +build tools

//go:generate go run honnef.co/go/tools/cmd/staticcheck -checks inherit,-SA1019 ./...

package tools

import (
	_ "honnef.co/go/tools/cmd/staticcheck"
)
