package main

import (
	"runtime/debug"
	"strings"
)

// resolvedVersion keeps explicit build injection authoritative, then uses the
// main module version only when the build has no VCS settings. Go stamps VCS
// metadata into ordinary source builds, including their revision as Main.Version;
// those builds retain the predictable dev fallback.
func resolvedVersion(override string) string {
	info, ok := debug.ReadBuildInfo()
	return versionFromBuildInfo(override, info, ok)
}

func versionFromBuildInfo(override string, info *debug.BuildInfo, ok bool) string {
	if override != "" {
		return override
	}
	if ok && info != nil && !hasVCSSettings(info) && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func hasVCSSettings(info *debug.BuildInfo) bool {
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs.") {
			return true
		}
	}
	return false
}
