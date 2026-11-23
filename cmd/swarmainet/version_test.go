// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	bi := func(v string, settings ...string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			b := &debug.BuildInfo{Main: debug.Module{Version: v}}
			for i := 0; i+1 < len(settings); i += 2 {
				b.Settings = append(b.Settings, debug.BuildSetting{Key: settings[i], Value: settings[i+1]})
			}
			return b, true
		}
	}
	for _, tc := range []struct {
		set  string
		info func() (*debug.BuildInfo, bool)
		want string
	}{
		{"v0.4.0", bi("(devel)"), "v0.4.0"},
		{"", bi("v0.4.0"), "v0.4.0"},
		{"", bi("(devel)", "vcs.revision", "78a6efca45609227", "vcs.modified", "false"), "dev-78a6efca"},
		{"", bi("v0.3.1-0.20261123-78a6efca4560+dirty", "vcs.revision", "78a6efca45609227", "vcs.modified", "true"), "dev-78a6efca-dirty"},
		{"", bi("(devel)"), "dev"},
		{"", func() (*debug.BuildInfo, bool) { return nil, false }, "dev"},
	} {
		if got := versionFrom(tc.set, tc.info); got != tc.want {
			t.Errorf("%q: %q, erwartet %q", tc.set, got, tc.want)
		}
	}
}
