// SPDX-License-Identifier: Apache-2.0

package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetUsesTheLinkerValues(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })
	Version, Commit = "1.2.3", "abc123"

	info := Get()
	if info.Version != "1.2.3" || info.Commit != "abc123" || info.Go != runtime.Version() {
		t.Fatalf("Get() = %+v", info)
	}
	if got := info.String(); !strings.Contains(got, "1.2.3") || !strings.Contains(got, "abc123") {
		t.Fatalf("String() = %q", got)
	}
}

func TestGetFillsDefaults(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })
	Version, Commit = "", ""

	info := Get()
	if info.Version == "" || info.Commit == "" || info.Go == "" {
		t.Fatalf("Get() left a field empty: %+v", info)
	}
}
