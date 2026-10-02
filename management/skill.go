// Package management exposes the canonical Board Management Skill embedded
// in the aboard binary.
package management

import _ "embed"

// Skill is the content of the canonical management/SKILL.md file.
//
//go:embed SKILL.md
var Skill string
