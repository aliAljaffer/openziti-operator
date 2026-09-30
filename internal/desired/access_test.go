// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"testing"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestCheckEntryRouters(t *testing.T) {
	conn := func(entry ...string) *zitiv1.ZitiConnection {
		return &zitiv1.ZitiConnection{Spec: zitiv1.ZitiConnectionSpec{EntryRouters: entry, DefaultEdgeRouters: []string{"dflt"}}}
	}
	for name, c := range map[string]struct {
		conn  *zitiv1.ZitiConnection
		names []string
		ok    bool
	}{
		"no allow-list allows all":  {conn(), []string{"any"}, true},
		"listed router":             {conn("edge"), []string{"edge"}, true},
		"default router is allowed": {conn("edge"), []string{"dflt"}, true},
		"unlisted router":           {conn("edge"), []string{"edge", "other"}, false},
	} {
		if err := CheckEntryRouters(c.conn, c.names); (err == nil) != c.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
