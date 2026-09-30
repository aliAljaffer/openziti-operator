// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/metrics"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func sweepSetup(t *testing.T, policy OrphanPolicy) (*OrphanSweeper, *ziti.Fake) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{ObjectMeta: metav1.ObjectMeta{Name: "default"}, Spec: zitiv1.ZitiConnectionSpec{ClusterID: "prod"}}
	app := &zitiv1.ZitiApp{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "n", UID: "live-uid"}}
	// One live owner of every other kind. Their entities must never count as orphans.
	owners := []client.Object{
		&zitiv1.ZitiIdentity{ObjectMeta: metav1.ObjectMeta{Name: "i", Namespace: "n", UID: "live-identity"}},
		&zitiv1.ZitiAccessPolicy{ObjectMeta: metav1.ObjectMeta{Name: "ap", Namespace: "n", UID: "live-accesspolicy"}},
		&zitiv1.ZitiJwtSigner{ObjectMeta: metav1.ObjectMeta{Name: "sg", UID: "live-signer"}},
		&zitiv1.ZitiCA{ObjectMeta: metav1.ObjectMeta{Name: "ca", UID: "live-ca"}},
		&zitiv1.ZitiRouter{ObjectMeta: metav1.ObjectMeta{Name: "rt", UID: "live-router"}},
		&zitiv1.ZitiConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "n", UID: "live-config"}},
		&zitiv1.ZitiService{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "n", UID: "live-service"}},
		&zitiv1.ZitiServicePolicy{ObjectMeta: metav1.ObjectMeta{Name: "sp", Namespace: "n", UID: "live-servicepolicy"}},
		&zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "erp", Namespace: "n", UID: "live-erp"}},
		&zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: metav1.ObjectMeta{Name: "serp", Namespace: "n", UID: "live-serp"}},
		&zitiv1.ZitiTerminator{ObjectMeta: metav1.ObjectMeta{Name: "term", Namespace: "n", UID: "live-terminator"}},
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(owners, conn, app)...).Build()
	zc := ziti.NewFake()

	tags := func(cluster, uid string, extra ...string) map[string]any {
		m := map[string]any{desired.TagCluster: cluster, desired.TagUID: uid, desired.TagKind: "ZitiApp", desired.TagNamespace: "n", desired.TagName: "gone"}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	zc.Put(ziti.Services, ziti.Entity{"id": "live", "name": "live", "tags": tags("prod", "live-uid")})
	zc.Put(ziti.Services, ziti.Entity{"id": "orphan", "name": "orphan", "tags": tags("prod", "dead-uid")})
	zc.Put(ziti.Configs, ziti.Entity{"id": "orphan-cfg", "name": "orphan-cfg", "configTypeId": "ct", "data": map[string]any{"k": "v"}, "tags": tags("prod", "dead-uid")})
	zc.Put(ziti.Identities, ziti.Entity{"id": "adopted", "name": "adopted", "tags": tags("prod", "dead-uid", desired.TagAdopted, "true", "owner", "human")})
	zc.Put(ziti.Services, ziti.Entity{"id": "other-cluster", "name": "other-cluster", "tags": tags("staging", "dead-uid")})
	zc.Put(ziti.Services, ziti.Entity{"id": "hand-made", "name": "hand-made", "tags": map[string]any{"owner": "human"}})
	for kind, uid := range map[ziti.Kind]string{
		ziti.ExternalJWTSigners: "live-signer", ziti.AuthPolicies: "live-signer", ziti.CertificateAuthorities: "live-ca",
		ziti.Configs: "live-config", ziti.ServicePolicies: "live-servicepolicy", ziti.EdgeRouterPolicies: "live-erp",
		ziti.ServiceEdgeRouterPolicies: "live-serp", ziti.Terminators: "live-terminator", ziti.Identities: "live-identity", ziti.EdgeRouters: "live-router",
	} {
		zc.Put(kind, ziti.Entity{"id": "live-" + string(kind), "name": "live-" + string(kind), "tags": tags("prod", uid)})
	}

	return &OrphanSweeper{Reader: k, Clients: staticProvider{zc}, Policy: policy}, zc
}

func TestSweeperReportsButDoesNotWrite(t *testing.T) {
	s, zc := sweepSetup(t, OrphanReport)
	if err := s.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, c := range zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("report policy wrote: %s", c)
		}
	}
	if got := testutil.ToFloat64(metrics.Orphans.WithLabelValues("services")); got != 1 {
		t.Errorf("orphaned services = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.Orphans.WithLabelValues("identities")); got != 1 {
		t.Errorf("orphaned identities = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.ManagedEntities.WithLabelValues("services")); got != 2 {
		t.Errorf("managed services = %v, want 2 (live and orphan, not the other cluster or hand-made)", got)
	}
}

func TestSweeperDeleteRemovesOrphansOnly(t *testing.T) {
	s, zc := sweepSetup(t, OrphanDelete)
	if err := s.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ziti.Kind{ziti.ExternalJWTSigners, ziti.AuthPolicies, ziti.CertificateAuthorities, ziti.Configs, ziti.ServicePolicies,
		ziti.EdgeRouterPolicies, ziti.ServiceEdgeRouterPolicies, ziti.Identities, ziti.EdgeRouters} {
		if _, ok := zc.Objects[kind]["live-"+string(kind)]; !ok {
			t.Errorf("the live %s entity was deleted", kind)
		}
	}
	for _, id := range []string{"live", "other-cluster", "hand-made"} {
		if _, ok := zc.Objects[ziti.Services][id]; !ok {
			t.Errorf("service %s was deleted", id)
		}
	}
	if _, ok := zc.Objects[ziti.Services]["orphan"]; ok {
		t.Error("orphaned service survived")
	}
	if _, ok := zc.Objects[ziti.Configs]["orphan-cfg"]; ok {
		t.Error("orphaned config survived")
	}
	adopted, ok := zc.Objects[ziti.Identities]["adopted"]
	if !ok {
		t.Fatal("adopted identity was deleted, it must be released")
	}
	if tags := adopted.Tags(); len(tags) != 1 || tags["owner"] != "human" {
		t.Errorf("adopted identity tags = %v", tags)
	}

	zc.Calls = nil
	if err := s.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, c := range zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("second sweep wrote: %s", c)
		}
	}
}
