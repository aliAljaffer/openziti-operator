//go:build integration

package controller

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestRouterAgainstRealController(t *testing.T) {
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

	e := setupRouter(t, func(r *zitiv1.ZitiRouter) { r.Spec.ZitiName = "it-router" })
	e.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		ctx := context.Background()
		var rt zitiv1.ZitiRouter
		if e.k.Get(ctx, e.key, &rt) == nil {
			_ = e.k.Delete(ctx, &rt)
			_, _ = e.r.Reconcile(ctx, ctrl.Request{NamespacedName: e.key})
		}
	})

	rt := e.reconcile(t)
	if rt.Status.RouterID == "" || rt.Status.Enrolled || rt.Status.EnrollmentExpiresAt == nil {
		t.Fatalf("status = %+v", rt.Status)
	}
	var secret corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-enrollment"}, &secret); err != nil {
		t.Fatal(err)
	}
	if jwt := string(secret.Data[SecretKeyJWT]); len(jwt) < 50 || jwt[:3] != "eyJ" {
		t.Fatalf("the Secret does not hold a JWT: %q", jwt)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	rt = e.reconcile(t)
	rt.Spec.RoleAttributes, rt.Spec.Cost = []string{"edge", "eu"}, 25
	if err := e.k.Update(t.Context(), rt); err != nil {
		t.Fatal(err)
	}
	wc.writes = nil
	e.reconcile(t)
	got, _ := real.List(t.Context(), ziti.EdgeRouters, `name="it-router"`)
	if len(got) != 1 || len(got[0]["roleAttributes"].([]any)) != 2 || got[0]["cost"] != float64(25) {
		t.Fatalf("update did not reach Ziti: %v", got)
	}
	if jwt, _, err := real.Enrollment(t.Context(), ziti.EdgeRouters, rt.Status.RouterID); err != nil || jwt == "" {
		t.Fatalf("an update must keep the pending enrollment: %v", err)
	}
	wc.writes = nil
	e.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("steady state wrote: %v", wc.writes)
	}
}

// Runs the docker-compose.yml that the operator writes to the Secret. Needs Docker and ZITI_DOCKER_NETWORK: the controller container on that network must advertise the name its
// router-address and ctrl-address use, so that the router container can resolve it.
func TestRouterEnrollsWithDeliveredJWT(t *testing.T) {
	mgmt, network := os.Getenv("ZITI_MGMT_URL"), os.Getenv("ZITI_DOCKER_NETWORK")
	if mgmt == "" || network == "" {
		t.Skip("ZITI_MGMT_URL or ZITI_DOCKER_NETWORK not set")
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
	e := setupRouter(t, func(r *zitiv1.ZitiRouter) {
		r.Spec.ZitiName, r.Spec.AdvertisedAddress = "it-enroll-router", "it-enroll-router"
	})
	e.r.Clients = staticProvider{real}
	version, err := real.Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var conn zitiv1.ZitiConnection
	if err := e.k.Get(t.Context(), types.NamespacedName{Name: "default"}, &conn); err != nil {
		t.Fatal(err)
	}
	conn.Status.ControllerVersion = version
	if err := e.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Cleanup(func() {
		ctx := context.Background()
		_ = exec.Command("docker", "compose", "-p", "it-enroll", "-f", dir+"/docker-compose.yml", "-f", dir+"/override.yml", "down", "-v").Run()
		var rt zitiv1.ZitiRouter
		if e.k.Get(ctx, e.key, &rt) == nil {
			_ = e.k.Delete(ctx, &rt)
			_, _ = e.r.Reconcile(ctx, ctrl.Request{NamespacedName: e.key})
		}
	})

	e.reconcile(t)
	var secret corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-enrollment"}, &secret); err != nil {
		t.Fatal(err)
	}
	compose := secret.Data[SecretKeyCompose]
	if len(compose) == 0 {
		t.Fatal("no docker-compose.yml delivered")
	}
	override := "services:\n  router:\n    container_name: it-enroll-router\n    networks: [default, zt]\nnetworks:\n  zt:\n    name: " + network + "\n    external: true\n"
	for name, data := range map[string]string{"docker-compose.yml": string(compose), "override.yml": override} {
		if err := os.WriteFile(dir+"/"+name, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("docker", "compose", "-p", "it-enroll", "-f", dir+"/docker-compose.yml", "-f", dir+"/override.yml", "up", "-d").CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose up: %v: %s", err, out)
	}

	var rt *zitiv1.ZitiRouter
	for range 45 {
		rt = e.reconcile(t)
		if rt.Status.Enrolled && rt.Status.Online {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !rt.Status.Enrolled || !rt.Status.Online {
		logs, _ := exec.Command("docker", "logs", "--tail", "30", "it-enroll-router").CombinedOutput()
		t.Fatalf("status = %+v\nrouter log:\n%s", rt.Status, logs)
	}
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "routers", Name: "edge-1-enrollment"}, &secret); err == nil {
		if _, ok := secret.Data[SecretKeyJWT]; ok {
			t.Error("the JWT must leave the Secret once the router has enrolled")
		}
	}
}
