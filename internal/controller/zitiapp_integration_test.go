//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/openziti/edge-api/rest_util"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestZitiAppAgainstRealController(t *testing.T) {
	mgmt := os.Getenv("ZITI_MGMT_URL")
	if mgmt == "" {
		t.Skip("ZITI_MGMT_URL not set")
	}
	u, _ := url.Parse(mgmt)
	pool, err := rest_util.GetControllerWellKnownCaPool("https://" + u.Host)
	if err != nil {
		t.Fatal(err)
	}
	auth := rest_util.NewAuthenticatorUpdb(os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD"))
	auth.RootCas = pool
	real, err := ziti.NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}
	wc := &writeCounter{Client: real}

	idID, err := real.Create(t.Context(), ziti.Identities, ziti.Entity{"name": "outside-k8s", "type": "Default", "isAdmin": false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Delete(context.Background(), ziti.Identities, idID) })

	e := setup(t, func(s *zitiv1.ZitiApp) {
		s.Spec.ZitiName = "it-app.example.com"
		s.Spec.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005")}
		s.Spec.Expose.Protocols = []string{"tcp", "udp"}
		s.Spec.Targets = []zitiv1.Target{{Address: "10.0.0.5"}, {Address: "10.0.0.6", Cost: 5}}
		s.Spec.Allow = zitiv1.Allow{Identities: []string{"outside-k8s", "not-yet"}}
		s.Spec.EntryRouters = []string{"router-instance-1"}
	})
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), typesName("default"), &conn); err != nil {
		t.Fatal(err)
	}
	conn.Spec.HostingRouters = []string{"router-instance-1"}
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	e.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		for _, k := range deleteOrder {
			list, _ := real.List(context.Background(), k, tagFilter("uid-1"))
			for _, x := range list {
				_ = real.Delete(context.Background(), k, x.ID())
			}
		}
	})

	e.reconcile(t)
	s := e.get(t)
	if s.Status.IDs.Service == "" || s.Status.IDs.ERP == "" || s.Status.IDs.Dial == "" {
		t.Fatalf("status = %+v", s.Status)
	}
	if c := meta.FindStatusCondition(s.Status.Conditions, CondAccess); c == nil || c.Reason != "IdentityNotFound" {
		t.Errorf("AccessResolved = %+v", c)
	}

	var terminators []ziti.Entity
	for range 20 {
		terminators, _ = real.List(t.Context(), ziti.Terminators, `service="`+s.Status.IDs.Service+`"`)
		if len(terminators) == 2 {
			break
		}
		time.Sleep(time.Second)
	}
	if len(terminators) != 2 {
		t.Fatalf("terminators = %d, want 2", len(terminators))
	}

	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}
	if got := e.get(t); len(got.Status.Terminators) != 2 || condStatus(got, CondHosted) != "True" || condStatus(got, CondDialable) != "True" {
		t.Errorf("status = %+v", got.Status)
	}

	if err := e.k.Delete(t.Context(), e.get(t)); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	for _, k := range deleteOrder {
		if list, _ := real.List(t.Context(), k, tagFilter("uid-1")); len(list) != 0 {
			t.Fatalf("%s left behind: %v", k, list)
		}
	}
}

func TestZitiAppAdoptAgainstRealController(t *testing.T) {
	mgmt := os.Getenv("ZITI_MGMT_URL")
	if mgmt == "" {
		t.Skip("ZITI_MGMT_URL not set")
	}
	u, _ := url.Parse(mgmt)
	pool, err := rest_util.GetControllerWellKnownCaPool("https://" + u.Host)
	if err != nil {
		t.Fatal(err)
	}
	auth := rest_util.NewAuthenticatorUpdb(os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD"))
	auth.RootCas = pool
	real, err := ziti.NewREST(mgmt, auth, 10)
	if err != nil {
		t.Fatal(err)
	}
	const name = "it-adopt.example.com"
	mut := func(uid types.UID, policy zitiv1.ManagementPolicy, del zitiv1.DeletionPolicy) func(*zitiv1.ZitiApp) {
		return func(s *zitiv1.ZitiApp) {
			s.UID = uid
			s.Spec.ZitiName = name
			s.Spec.ManagementPolicy = policy
			s.Spec.DeletionPolicy = del
			s.Spec.Targets = []zitiv1.Target{{Address: "10.0.0.5"}}
		}
	}
	useRouter := func(e *env, wc *writeCounter) {
		var conn zitiv1.ZitiConnection
		if err := e.k.Get(t.Context(), typesName("default"), &conn); err != nil {
			t.Fatal(err)
		}
		conn.Spec.HostingRouters = []string{"router-instance-1"}
		if err := e.k.Update(t.Context(), &conn); err != nil {
			t.Fatal(err)
		}
		e.r.Clients = staticProvider{wc}
	}
	t.Cleanup(func() {
		for _, k := range deleteOrder {
			list, _ := real.List(context.Background(), k, `name contains "`+name+`"`)
			for _, x := range list {
				_ = real.Delete(context.Background(), k, x.ID())
			}
		}
	})

	// Make hand-made entities: create as an app, then delete it with Orphan so the entities lose their tags.
	first := setup(t, mut("uid-first", zitiv1.ManagementManage, zitiv1.DeletionPolicyOrphan))
	useRouter(first, &writeCounter{Client: real})
	first.reconcile(t)
	origID := first.get(t).Status.IDs.Service
	if origID == "" {
		t.Fatalf("status = %+v", first.get(t).Status)
	}
	if err := first.k.Delete(t.Context(), first.get(t)); err != nil {
		t.Fatal(err)
	}
	first.reconcile(t)
	if list, _ := real.List(t.Context(), ziti.Services, `name="`+name+`"`); len(list) != 1 || list[0].Tags()["ziti-operator-uid"] != nil {
		t.Fatalf("hand-made service not released: %v", list)
	}

	wc := &writeCounter{Client: real}
	e := setup(t, mut("uid-adopt", zitiv1.ManagementAdopt, zitiv1.DeletionPolicyDelete))
	useRouter(e, wc)
	e.reconcile(t)
	s := e.get(t)
	if s.Status.IDs.Service != origID {
		t.Fatalf("service id = %q, want adopted %q; status %+v", s.Status.IDs.Service, origID, s.Status)
	}
	if list, _ := real.List(t.Context(), ziti.Services, `name="`+name+`"`); len(list) != 1 || list[0].Tags()["ziti-operator-adopted"] != "true" || list[0].Tags()["ziti-operator-uid"] != "uid-adopt" {
		t.Fatalf("service not adopted: %v", list)
	}

	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	if err := e.k.Delete(t.Context(), e.get(t)); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	list, _ := real.List(t.Context(), ziti.Services, `name="`+name+`"`)
	if len(list) != 1 || list[0].Tags()["ziti-operator-uid"] != nil {
		t.Fatalf("adopted service was deleted or kept its tags: %v", list)
	}
}
