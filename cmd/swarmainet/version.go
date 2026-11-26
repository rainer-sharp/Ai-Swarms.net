// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

package main

import (
	"runtime/debug"
	"strings"
)

// version setzt der Release-Build: -ldflags "-X main.version=v0.4.0".
var version string

// versionString nennt die Version: aus dem Release-Build, sonst aus den
// Moduldaten (go install …@v0.4.0), sonst den Git-Stand eines eigenen
// Builds („dev-1a2b3c4d“, mit „-dirty“ bei lokalen Änderungen).
func versionString() string {
	return versionFrom(version, readBuildInfo)
}

var readBuildInfo = debug.ReadBuildInfo

func versionFrom(set string, info func() (*debug.BuildInfo, bool)) string {
	if set != "" {
		return set
	}
	bi, ok := info()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" && !strings.Contains(v, "+dirty") {
		return v
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	v := "dev-" + rev[:min(8, len(rev))]
	if dirty {
		v += "-dirty"
	}
	return v
}
