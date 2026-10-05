//go:build integration

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func TestCAAndCertificateLoginAgainstRealController(t *testing.T) {
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

	secret := caSecret(t, true, true)
	ce := setupCA(t, secret, func(ca *zitiv1.ZitiCA) {
		ca.Spec.ZitiName = "it-ca"
		ca.Spec.Verification.SignWithSecretKey = true
	})
	ce.r.Clients = staticProvider{wc}
	t.Cleanup(func() {
		ctx := context.Background()
		var ca zitiv1.ZitiCA
		if ce.k.Get(ctx, ce.key, &ca) == nil {
			_ = ce.k.Delete(ctx, &ca)
			_, _ = ce.r.Reconcile(ctx, ctrl.Request{NamespacedName: ce.key})
		}
	})

	ca := ce.reconcile(t)
	if !ca.Status.Verified || ca.Status.CAID == "" || condOf(ca, CondReady).Status != "True" {
		t.Fatalf("status = %+v conditions = %+v", ca.Status, ca.Status.Conditions)
	}
	wc.writes = nil
	ce.reconcile(t)
	if len(wc.writes) != 0 {
		t.Fatalf("second reconcile wrote: %v", wc.writes)
	}

	// The issuer certificate is renewed: same key, new certificate. The CA must follow and stay verified.
	oldFingerprint := ca.Status.Fingerprint
	caKeyForRenewal, _ := ziti.ParsePrivateKey(secret.Data[corev1.TLSPrivateKeyKey])
	oldCert, _, _ := ziti.FirstCertificate(secret.Data[corev1.TLSCertKey])
	renewedTmpl := &x509.Certificate{SerialNumber: big.NewInt(4242), Subject: oldCert.Subject, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	renewedDER, err := x509.CreateCertificate(rand.Reader, renewedTmpl, renewedTmpl, caKeyForRenewal.Public(), caKeyForRenewal)
	if err != nil {
		t.Fatal(err)
	}
	var live corev1.Secret
	if err := ce.k.Get(t.Context(), typesName2("cert-manager", "issuer-ca"), &live); err != nil {
		t.Fatal(err)
	}
	live.Data[corev1.TLSCertKey] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: renewedDER})
	if err := ce.k.Update(t.Context(), &live); err != nil {
		t.Fatal(err)
	}
	renewed := ce.reconcile(t)
	if renewed.Status.Fingerprint == oldFingerprint || !renewed.Status.Verified || condOf(renewed, CondReady).Status != "True" {
		t.Fatalf("renewal: fingerprint %s -> %s, verified %v, conditions %+v", oldFingerprint, renewed.Status.Fingerprint, renewed.Status.Verified, renewed.Status.Conditions)
	}
	secret = &live

	ie := setupIdentity(t)
	ie.r.Clients = staticProvider{wc}
	var conn zitiv1.ZitiConnection
	if err := ie.k.Get(t.Context(), typesName("default"), &conn); err != nil {
		t.Fatal(err)
	}
	conn.Spec.RoleScope = zitiv1.RoleScopeGlobal
	if err := ie.k.Update(t.Context(), &conn); err != nil {
		t.Fatal(err)
	}
	setSpec(t, ie, func(s *zitiv1.ZitiIdentitySpec) {
		s.EnrollmentMode, s.AuthPolicy, s.ExternalID = zitiv1.EnrollmentNone, "Default", "workload-a"
	})
	t.Cleanup(func() {
		ctx := context.Background()
		var cr zitiv1.ZitiIdentity
		if ie.k.Get(ctx, ie.key, &cr) == nil {
			_ = ie.k.Delete(ctx, &cr)
			_, _ = ie.r.Reconcile(ctx, ctrl.Request{NamespacedName: ie.key})
		}
	})
	ie.reconcile(t)

	caCert, _, _ := ziti.FirstCertificate(secret.Data[corev1.TLSCertKey])
	caKey, _ := ziti.ParsePrivateKey(secret.Data[corev1.TLSPrivateKeyKey])
	clientCert := func(cn string) tls.Certificate {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}
	login := func(cn string) (int, string) {
		hc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: []tls.Certificate{clientCert(cn)}}}}
		resp, err := hc.Post("https://"+u.Host+"/edge/client/v1/authenticate?method=cert", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Data struct {
				Identity struct{ Name string } `json:"identity"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Data.Identity.Name
	}
	if code, name := login("workload-a"); code != http.StatusOK || name != "default-team-a-backend" {
		t.Errorf("certificate of the identity: status %d, identity %q", code, name)
	}
	if code, _ := login("someone-else"); code == http.StatusOK {
		t.Error("a certificate without an identity must be rejected")
	}
}
