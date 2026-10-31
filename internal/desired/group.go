// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func GroupRoleAttribute(g *zitiv1.ZitiGroup) string {
	if g.Spec.RoleAttribute != "" {
		return g.Spec.RoleAttribute
	}
	return "group." + g.Name
}

func GroupHasIdentity(g *zitiv1.ZitiGroup, id *zitiv1.ZitiIdentity) bool {
	if slices.Contains(g.Spec.Members, id.Name) {
		return true
	}
	if g.Spec.Selector == nil {
		return false
	}
	sel, err := metav1.LabelSelectorAsSelector(g.Spec.Selector)
	if err != nil {
		return false
	}
	return sel.Matches(labels.Set(id.Labels))
}

func GroupMembers(g *zitiv1.ZitiGroup, ids []zitiv1.ZitiIdentity) []string {
	out := make([]string, 0, len(ids))
	for i := range ids {
		if GroupHasIdentity(g, &ids[i]) {
			out = append(out, ids[i].Name)
		}
	}
	slices.Sort(out)
	return out
}
