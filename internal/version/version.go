// SPDX-License-Identifier: Apache-2.0

// Package version reports the build version of the operator.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version and Commit are set with -ldflags at release build time. When they are empty, Get reads
// the values that the Go toolchain recorded in the binary.
var (
	Version string
	Commit  string
)

// Info is the build information reported by --version and the build info metric.
type Info struct {
	Version string
	Commit  string
	Go      string
}

func (i Info) String() string {
	return fmt.Sprintf("%s (commit %s, %s)", i.Version, i.Commit, i.Go)
}

// Get returns the build information with useful defaults for a development build.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Go: runtime.Version()}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" {
			info.Version = bi.Main.Version
		}
		for _, setting := range bi.Settings {
			if setting.Key == "vcs.revision" && info.Commit == "" {
				info.Commit = setting.Value
			}
		}
	}
	if info.Version == "" || info.Version == "(devel)" {
		info.Version = "devel"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
}
