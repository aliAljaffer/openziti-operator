// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type caEnv struct {
	r   *ZitiCAReconciler
	zc  *ziti.Fake
	k   client.Client
	key types.NamespacedName
}

func caSecret(t *testing.T, isCA bool, withKey bool) *corev1.Secret {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "issuer"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: isCA, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "issuer-ca", Namespace: "cert-manager"},
		Data: map[string][]byte{corev1.TLSCertKey: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}}
	if withKey {
		der, _ := x509.MarshalECPrivateKey(key)
		s.Data[corev1.TLSPrivateKeyKey] = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	}
	return s
}

func setupCA(t *testing.T, secret *corev1.Secret, mut func(*zitiv1.ZitiCA)) *caEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	ca := &zitiv1.ZitiCA{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", UID: "uid-ca", Generation: 1},
		Spec: zitiv1.ZitiCASpec{
			ConnectionRef: "default", DeletionPolicy: zitiv1.DeletionPolicyDelete,
			Certificate: zitiv1.CertificateSource{SecretRef: zitiv1.SecretRef{Namespace: "cert-manager", Name: "issuer-ca"}},
		},
	}
	if mut != nil {
		mut(ca)
	}
	objs := []client.Object{conn, ca}
	if secret != nil {
		objs = append(objs, secret)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&zitiv1.ZitiCA{}).Build()
	zc := ziti.NewFake()
	return &caEnv{
		r:  &ZitiCAReconciler{Client: k, Reader: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(50)},
		zc: zc, k: k, key: types.NamespacedName{Name: "cm"},
	}
}

func (e *caEnv) reconcile(t *testing.T) *zitiv1.ZitiCA {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var ca zitiv1.ZitiCA
	if err := e.k.Get(t.Context(), e.key, &ca); err != nil {
		return nil
	}
	return &ca
}

func condOf(ca *zitiv1.ZitiCA, typ string) metav1.Condition {
	for _, c := range ca.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestCAWithoutTheKeyWaitsForManualVerification(t *testing.T) {
	e := setupCA(t, caSecret(t, true, true), nil)
	ca := e.reconcile(t)

	entity := only(e.zc.Objects[ziti.CertificateAuthorities])
	if entity.Name() != "cm" || entity["isAuthEnabled"] != true || entity["isAutoCaEnrollmentEnabled"] != false ||
		entity["identityNameFormat"] != "[caName]-[commonName]" {
		t.Errorf("entity = %v", entity)
	}
	claim := entity["externalIdClaim"].(map[string]any)
	if claim["location"] != "COMMON_NAME" || claim["matcher"] != "ALL" || claim["parser"] != "NONE" {
		t.Errorf("externalIdClaim = %v", claim)
	}
	if ca.Status.Verified || ca.Status.VerificationToken == "" || ca.Status.CAID != entity.ID() {
		t.Errorf("status = %+v", ca.Status)
	}
	if c := condOf(ca, CondVerified); c.Reason != "AwaitingVerification" || !strings.Contains(c.Message, ca.Status.VerificationToken) {
		t.Errorf("verified = %+v", c)
	}
	if condOf(ca, CondReady).Status != metav1.ConditionFalse {
		t.Error("an unverified CA is not ready")
	}
	for _, c := range e.zc.Calls {
		if strings.HasPrefix(c, "verify") {
			t.Error("the operator must not sign anything without signWithSecretKey")
		}
	}
}

func TestCASignsTheProofWithTheSecretKey(t *testing.T) {
	e := setupCA(t, caSecret(t, true, true), func(ca *zitiv1.ZitiCA) { ca.Spec.Verification.SignWithSecretKey = true })
	ca := e.reconcile(t)
	if !ca.Status.Verified || ca.Status.VerificationToken != "" || condOf(ca, CondReady).Status != metav1.ConditionTrue {
		t.Fatalf("status = %+v conditions = %+v", ca.Status, ca.Status.Conditions)
	}
	if ca.Status.Fingerprint == "" {
		t.Error("the fingerprint must reach status")
	}

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("second reconcile wrote: %s", c)
		}
	}
}

func TestCAWithTheWrongKeyFailsVerificationButStaysSynced(t *testing.T) {
	secret := caSecret(t, true, false)
	other := caSecret(t, true, true)
	secret.Data[corev1.TLSPrivateKeyKey] = other.Data[corev1.TLSPrivateKeyKey]
	e := setupCA(t, secret, func(ca *zitiv1.ZitiCA) { ca.Spec.Verification.SignWithSecretKey = true })
	ca := e.reconcile(t)
	if condOf(ca, CondSynced).Status != metav1.ConditionTrue || condOf(ca, CondVerified).Reason != "VerificationFailed" || ca.Status.Verified {
		t.Errorf("conditions = %+v", ca.Status.Conditions)
	}
	if len(e.zc.Objects[ziti.CertificateAuthorities]) != 1 {
		t.Error("the CA itself must still exist in Ziti")
	}
}

func TestCAWithoutAKeyInTheSecretExplainsIt(t *testing.T) {
	e := setupCA(t, caSecret(t, true, false), func(ca *zitiv1.ZitiCA) { ca.Spec.Verification.SignWithSecretKey = true })
	c := condOf(e.reconcile(t), CondVerified)
	if c.Reason != "VerificationFailed" || !strings.Contains(c.Message, "tls.key") {
		t.Errorf("verified = %+v", c)
	}
}

func TestCAInputErrors(t *testing.T) {
	e := setupCA(t, caSecret(t, false, false), nil)
	if c := condOf(e.reconcile(t), CondSynced); c.Reason != "InvalidSpec" || !strings.Contains(c.Message, "not a CA certificate") {
		t.Errorf("leaf certificate: %+v", c)
	}
	bad := caSecret(t, true, false)
	bad.Data[corev1.TLSCertKey] = []byte("junk")
	e = setupCA(t, bad, nil)
	if c := condOf(e.reconcile(t), CondSynced); c.Reason != "InvalidSpec" {
		t.Errorf("junk: %+v", c)
	}
	e = setupCA(t, nil, nil)
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err == nil {
		t.Error("a missing Secret may appear later, so it must be retried")
	}
}

func TestCAAutoEnrollmentAndClaimReachZiti(t *testing.T) {
	e := setupCA(t, caSecret(t, true, false), func(ca *zitiv1.ZitiCA) {
		ca.Spec.ExternalIDClaim = zitiv1.ExternalIDClaim{Location: "SAN_URI", Matcher: "SCHEME", MatcherCriteria: "spiffe", Parser: "SPLIT", ParserCriteria: "/", Index: 2}
		ca.Spec.AutoEnrollment = &zitiv1.AutoEnrollment{IdentityRoles: []string{"auto"}, IdentityNameFormat: "[commonName]"}
		off := false
		ca.Spec.AuthEnabled = &off
	})
	e.reconcile(t)
	entity := only(e.zc.Objects[ziti.CertificateAuthorities])
	claim := entity["externalIdClaim"].(map[string]any)
	if entity["isAutoCaEnrollmentEnabled"] != true || entity["isAuthEnabled"] != false || entity["identityNameFormat"] != "[commonName]" ||
		claim["location"] != "SAN_URI" || claim["matcherCriteria"] != "spiffe" || claim["parserCriteria"] != "/" {
		t.Errorf("entity = %v", entity)
	}
}

func TestCADeleteAndOrphan(t *testing.T) {
	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := setupCA(t, caSecret(t, true, false), func(ca *zitiv1.ZitiCA) { ca.Spec.DeletionPolicy = policy })
		ca := e.reconcile(t)
		if err := e.k.Delete(t.Context(), ca); err != nil {
			t.Fatal(err)
		}
		e.reconcile(t)
		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
		}
		if n := len(e.zc.Objects[ziti.CertificateAuthorities]); n != want {
			t.Errorf("%s: CAs = %d, want %d", policy, n, want)
		}
		for _, o := range e.zc.Objects[ziti.CertificateAuthorities] {
			if len(o.Tags()) != 0 {
				t.Errorf("released CA keeps tags %v", o.Tags())
			}
		}
	}
}

func TestCARenewalReplacesTheEntityAndVerifiesAgain(t *testing.T) {
	first := caSecret(t, true, true)
	e := setupCA(t, first, func(ca *zitiv1.ZitiCA) { ca.Spec.Verification.SignWithSecretKey = true })
	ca := e.reconcile(t)
	oldID, oldFingerprint := ca.Status.CAID, ca.Status.Fingerprint

	key, _ := ziti.ParsePrivateKey(first.Data[corev1.TLSPrivateKeyKey])
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(99), Subject: pkix.Name{CommonName: "issuer"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	var live corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "cert-manager", Name: "issuer-ca"}, &live); err != nil {
		t.Fatal(err)
	}
	live.Data[corev1.TLSCertKey] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := e.k.Update(t.Context(), &live); err != nil {
		t.Fatal(err)
	}

	renewed := e.reconcile(t)
	if len(e.zc.Objects[ziti.CertificateAuthorities]) != 1 {
		t.Fatalf("CAs = %d, the old one must be replaced", len(e.zc.Objects[ziti.CertificateAuthorities]))
	}
	if renewed.Status.CAID == oldID || renewed.Status.Fingerprint == oldFingerprint {
		t.Errorf("the CA was not replaced: id %s -> %s", oldID, renewed.Status.CAID)
	}
	if !renewed.Status.Verified || condOf(renewed, CondReady).Status != metav1.ConditionTrue {
		t.Errorf("the new CA must be verified again: %+v", renewed.Status)
	}

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("a steady CA must not be written: %s", c)
		}
	}
}

func TestCAVerifiesThroughCertManagerWithoutReadingTheKey(t *testing.T) {
	caSec := caSecret(t, true, true)
	e := setupCA(t, caSec, func(ca *zitiv1.ZitiCA) {
		ca.Spec.Verification.IssuerRef = &zitiv1.VerificationIssuer{Name: "workloads", Namespace: "cert-manager"}
	})
	ca := e.reconcile(t)
	if c := condOf(ca, CondVerified); c.Reason != "AwaitingCertificate" {
		t.Fatalf("verified = %+v", c)
	}
	token := only(e.zc.Objects[ziti.CertificateAuthorities])["verificationToken"].(string)

	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"})
	key := types.NamespacedName{Namespace: "cert-manager", Name: "cm-verify"}
	if err := e.k.Get(t.Context(), key, cert); err != nil {
		t.Fatal(err)
	}
	spec, _ := cert.Object["spec"].(map[string]any)
	if spec["commonName"] != token || spec["secretName"] != "cm-verify" {
		t.Errorf("spec = %v", spec)
	}

	// cert-manager issues the certificate: Ready and a Secret that holds a leaf with the token as common name.
	caCert, _, _ := ziti.FirstCertificate(caSec.Data[corev1.TLSCertKey])
	caKey, _ := ziti.ParsePrivateKey(caSec.Data[corev1.TLSPrivateKeyKey])
	proof, err := ziti.ProofCertificate(token, caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}
	if err := e.k.Update(t.Context(), cert); err != nil {
		t.Fatal(err)
	}
	leafSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "cert-manager", Name: "cm-verify",
		Annotations: map[string]string{"cert-manager.io/certificate-name": "cm-verify"}}, Data: map[string][]byte{corev1.TLSCertKey: []byte(proof)}}
	if err := e.k.Create(t.Context(), leafSecret); err != nil {
		t.Fatal(err)
	}

	ca = e.reconcile(t)
	if !ca.Status.Verified || condOf(ca, CondReady).Status != metav1.ConditionTrue {
		t.Fatalf("status = %+v, conditions %+v", ca.Status, ca.Status.Conditions)
	}
	if err := e.k.Get(t.Context(), key, cert); !apierrors.IsNotFound(err) {
		t.Errorf("the proof Certificate must be removed: %v", err)
	}
	if err := e.k.Get(t.Context(), key, &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Errorf("the proof Secret must be removed: %v", err)
	}
	var kept corev1.Secret
	if err := e.k.Get(t.Context(), types.NamespacedName{Namespace: "cert-manager", Name: "issuer-ca"}, &kept); err != nil {
		t.Errorf("the CA Secret must stay: %v", err)
	}
}
