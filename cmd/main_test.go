// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"
	"testing"
)

func TestSecretCacheNamespaces(t *testing.T) {
	if got := secretCache("").Namespaces; len(got) != 0 {
		t.Errorf("empty flag must not limit namespaces: %v", got)
	}
	got := secretCache(" a, b ,,c").Namespaces
	var names []string
	for ns := range got {
		names = append(names, ns)
	}
	sort.Strings(names)
	if len(names) != 3 || names[0] != "a" || names[2] != "c" {
		t.Errorf("namespaces = %v", names)
	}
	if secretCache("").Label == nil || secretCache("a").Label.String() != "app.kubernetes.io/managed-by=ziti-operator" {
		t.Error("the cache must always filter on the operator label")
	}
}
