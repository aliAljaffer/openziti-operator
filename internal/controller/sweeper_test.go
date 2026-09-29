// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn, app).Build()
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
