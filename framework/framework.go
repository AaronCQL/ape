// Package framework holds the files ape writes into a project, embedded in the binary.
package framework

import (
	_ "embed"
	"strings"
)

// Version is ape's version, written into _FRAMEWORK.md's first line.
const Version = "v0.6.0"

//go:embed _FRAMEWORK.md
var frameworkMD string

//go:embed templates/PROJECT.md
var ProjectMD []byte

//go:embed templates/DECISIONS.md
var DecisionsMD []byte

//go:embed templates/setup-task.md
var SetupTask []byte

//go:embed templates/agents-pointer.md
var AgentsPointer []byte

// FrameworkMD is _FRAMEWORK.md as ape init writes it.
func FrameworkMD() []byte {
	return []byte(strings.ReplaceAll(frameworkMD, "{{VERSION}}", Version))
}
