// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"net/http"
	"strings"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// ZitiReadiness reports whether the operator can reach at least one Ziti controller. With no
// ZitiConnection yet, it reports ready so a fresh install can create the first one.
type ZitiReadiness struct{ Reader client.Reader }

func (r *ZitiReadiness) Check(req *http.Request) error {
	var list zitiv1.ZitiConnectionList
	if err := r.Reader.List(req.Context(), &list); err != nil {
		return fmt.Errorf("list ZitiConnections: %w", err)
	}
	if len(list.Items) == 0 {
		return nil
	}

	var notConnected []string
	for i := range list.Items {
		cond := apimeta.FindStatusCondition(list.Items[i].Status.Conditions, ConditionConnected)
		if cond != nil && cond.Status == metav1.ConditionTrue {
			return nil
		}
		notConnected = append(notConnected, list.Items[i].Name)
	}
	return fmt.Errorf("no ZitiConnection is connected: %s", strings.Join(notConnected, ", "))
}
