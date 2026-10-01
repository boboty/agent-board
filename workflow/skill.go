// Package workflow exposes the canonical Workflow Skill embedded in the
// agent-board binary.
package workflow

import _ "embed"

// Skill is the content of the canonical workflow/SKILL.md file.
//
//go:embed SKILL.md
var Skill string
