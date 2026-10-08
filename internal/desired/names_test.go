package desired

import (
	"testing"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDefaultNames(t *testing.T) {
	conn := &zitiv1.ZitiConnection{Spec: zitiv1.ZitiConnectionSpec{ClusterID: "prod"}}
	meta := metav1.ObjectMeta{Namespace: "shop", Name: "web"}

	id := &zitiv1.ZitiIdentity{ObjectMeta: meta}
	if got := IdentityName(id, conn); got != "prod-shop-web" {
		t.Errorf("new identity = %q", got)
	}
	id.Status.ZitiID = "abc"
	if got := IdentityName(id, conn); got != "shop.web" {
		t.Errorf("legacy identity = %q", got)
	}
	id.Status.ZitiName = "prod-shop-web"
	if got := IdentityName(id, conn); got != "prod-shop-web" {
		t.Errorf("stored identity = %q", got)
	}
	id.Spec.ZitiName = "custom"
	if got := IdentityName(id, conn); got != "custom" {
		t.Errorf("explicit identity = %q", got)
	}

	rt := &zitiv1.ZitiRouter{Name: "edge-1"}
	if got := RouterName(rt, conn); got != "prod-edge-1" {
		t.Errorf("new router = %q", got)
	}
	rt.Status.RouterID = "r1"
	if got := RouterName(rt, conn); got != "edge-1" {
		t.Errorf("legacy router = %q", got)
	}
}
