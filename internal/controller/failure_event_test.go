// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

func TestMarkFailedEmitsWarningEvent(t *testing.T) {
	rec := record.NewFakeRecorder(1)
	obj := &zitiv1.ZitiApp{}
	obj.Namespace, obj.Name = "team-a", "billing"
	var conds []metav1.Condition

	markFailed(rec, obj, &conds, 7, "InvalidSpec", "port 0 is not allowed")

	select {
	case event := <-rec.Events:
		for _, want := range []string{corev1.EventTypeWarning, "InvalidSpec", "port 0 is not allowed", "Fix the spec"} {
			if !strings.Contains(event, want) {
				t.Errorf("event %q does not contain %q", event, want)
			}
		}
	default:
		t.Fatal("no event was recorded")
	}

	for _, condType := range []string{CondSynced, CondReady} {
		c := apimeta.FindStatusCondition(conds, condType)
		if c == nil || c.Status != metav1.ConditionFalse || c.Reason != "InvalidSpec" || c.ObservedGeneration != 7 ||
			!strings.Contains(c.Message, "Fix the spec") {
			t.Errorf("%s condition = %+v", condType, c)
		}
	}
}

func TestMarkFailedWithoutRecorder(t *testing.T) {
	var conds []metav1.Condition
	markFailed(nil, &zitiv1.ZitiApp{}, &conds, 1, "Error", "boom")

	if c := apimeta.FindStatusCondition(conds, CondReady); c == nil || c.Reason != "Error" {
		t.Errorf("Ready condition = %+v", c)
	}
}
