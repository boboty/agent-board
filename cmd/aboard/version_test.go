package main

import (
	"runtime/debug"
	"testing"
)

func TestResolvedVersion(t *testing.T) {
	tests := []struct {
		name     string
		override string
		info     *debug.BuildInfo
		ok       bool
		want     string
	}{
		{name: "override wins", override: "v9.8.7", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}, Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}}, ok: true, want: "v9.8.7"},
		{name: "explicit dev override wins", override: "dev", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, ok: true, want: "dev"},
		{name: "module version", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, ok: true, want: "v1.2.3"},
		{name: "clean VCS checkout falls back", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261002073245-f393b6d75db6"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "f393b6d75db6"}, {Key: "vcs.modified", Value: "false"}}}, ok: true, want: "dev"},
		{name: "dirty VCS checkout falls back", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261002073245-f393b6d75db6+dirty"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "f393b6d75db6"}, {Key: "vcs.modified", Value: "true"}}}, ok: true, want: "dev"},
		{name: "devel fallback", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true, want: "dev"},
		{name: "missing version fallback", info: &debug.BuildInfo{Main: debug.Module{}}, ok: true, want: "dev"},
		{name: "unavailable build info fallback", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, ok: false, want: "dev"},
		{name: "nil build info fallback", ok: true, want: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFromBuildInfo(tt.override, tt.info, tt.ok); got != tt.want {
				t.Fatalf("resolvedVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
