// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"slices"
	"testing"
)

func TestSecretCacheNamespaces(t *testing.T) {
	if got := secretCache("").Namespaces; len(got) != 0 {
		t.Errorf("empty flag must not limit namespaces: %v", got)
	}
	got := secretCache(" a, b ,,c").Namespaces
	names := make([]string, 0, len(got))
	for ns := range got {
		names = append(names, ns)
	}
	slices.Sort(names)
	if len(names) != 3 || names[0] != "a" || names[2] != "c" {
		t.Errorf("namespaces = %v", names)
	}
	if secretCache("").Label == nil || secretCache("a").Label.String() != "app.kubernetes.io/managed-by=ziti-operator" {
		t.Error("the cache must always filter on the operator label")
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{" , ,", nil},
		{"a", []string{"a"}},
		{" a, b ,,c ", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		if got := splitList(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitList(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestOperatorNamespaceFromEnv(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE", "ziti-system")
	if got := operatorNamespace(); got != "ziti-system" {
		t.Errorf("got %q", got)
	}
}
