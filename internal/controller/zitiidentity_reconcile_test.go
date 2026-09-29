// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type idEnv struct {
	r   *ZitiIdentityReconciler
	zc  *ziti.Fake
	k   client.Client
	rec *record.FakeRecorder
	key types.NamespacedName
}

func setupIdentity(t *testing.T, objs ...client.Object) *idEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec:       zitiv1.ZitiConnectionSpec{RoleScope: zitiv1.RoleScopeNamespaced},
	}
	zid := &zitiv1.ZitiIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "team-a", UID: "uid-1", Generation: 1},
		Spec: zitiv1.ZitiIdentitySpec{
			ConnectionRef: "default", RoleAttributes: []string{"web"}, DeletionPolicy: zitiv1.DeletionPolicyDelete,
		},
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objs, conn, zid)...).
		WithStatusSubresource(&zitiv1.ZitiIdentity{}).Build()
	zc := ziti.NewFake()
	zc.Put(ziti.AuthPolicies, ziti.Entity{"id": "default", "name": "Default"})
	rec := record.NewFakeRecorder(100)
	return &idEnv{
		r:  &ZitiIdentityReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: rec},
		zc: zc, k: k, rec: rec,
		key: types.NamespacedName{Namespace: "team-a", Name: "backend"},
	}
}

func (e *idEnv) reconcile(t *testing.T) {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func (e *idEnv) get(t *testing.T) *zitiv1.ZitiIdentity {
	t.Helper()
	var z zitiv1.ZitiIdentity
	if err := e.k.Get(t.Context(), e.key, &z); err != nil {
		t.Fatal(err)
	}
	return &z
}

func (e *idEnv) secret(t *testing.T) *corev1.Secret {
	t.Helper()
	var s corev1.Secret
	if err := e.k.Get(t.Context(), e.key, &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func (e *idEnv) identity() ziti.Entity {
	for _, v := range e.zc.Objects[ziti.Identities] {
		return v
	}
	return nil
}

func TestIdentityCreatesAndPublishesJWTOnly(t *testing.T) {
	e := setupIdentity(t)
	e.reconcile(t)

	z := e.get(t)
	body := e.identity()
	if body.Name() != "team-a.backend" || body["type"] != "Default" || body["isAdmin"] != false {
		t.Fatalf("identity = %v", body)
	}
	if attrs, _ := json.Marshal(body["roleAttributes"]); string(attrs) != `["team-a.web"]` {
		t.Errorf("roleAttributes = %s", attrs)
	}
	if z.Status.ZitiID != body.ID() || z.Status.Enrolled || z.Status.EnrollmentExpiresAt == nil {
		t.Errorf("status = %+v", z.Status)
	}
	if condStatusOf(z, CondReady) != metav1.ConditionFalse || condStatusOf(z, CondSynced) != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", z.Status.Conditions)
	}

	s := e.secret(t)
	jwt := string(s.Data[SecretKeyJWT])
	if jwt == "" || !metav1.IsControlledBy(s, z) {
		t.Fatalf("secret = %+v", s)
	}
	raw, _ := json.Marshal(z.Status)
	if strings.Contains(string(raw), jwt) {
		t.Error("jwt leaked into status")
	}
	for len(e.rec.Events) > 0 {
		if strings.Contains(<-e.rec.Events, jwt) {
			t.Error("jwt leaked into an event")
		}
	}

	e.zc.Calls = nil
	e.reconcile(t)
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("second reconcile wrote: %s", c)
		}
	}
}

func condStatusOf(z *zitiv1.ZitiIdentity, typ string) metav1.ConditionStatus {
	for _, c := range z.Status.Conditions {
		if c.Type == typ {
			return c.Status
		}
	}
	return "missing"
}

func TestIdentityExpiredEnrollmentIsRecreated(t *testing.T) {
	e := setupIdentity(t)
	e.reconcile(t)
	old := string(e.secret(t).Data[SecretKeyJWT])
	for _, en := range e.zc.Objects[ziti.Enrollments] {
		en["expiresAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	}
	e.reconcile(t)

	if len(e.zc.Objects[ziti.Enrollments]) != 1 {
		t.Fatalf("enrollments = %d", len(e.zc.Objects[ziti.Enrollments]))
	}
	if got := string(e.secret(t).Data[SecretKeyJWT]); got == "" || got == old {
		t.Errorf("jwt not refreshed: %q", got)
	}
}

func TestIdentityEnrolledDropsJWT(t *testing.T) {
	e := setupIdentity(t)
	e.reconcile(t)
	e.identity()["authenticators"] = map[string]any{"cert": map[string]any{"id": "a1"}}
	e.reconcile(t)

	z := e.get(t)
	if !z.Status.Enrolled || condStatusOf(z, CondReady) != metav1.ConditionTrue || z.Status.EnrollmentExpiresAt != nil {
		t.Errorf("status = %+v", z.Status)
	}
	if _, ok := e.secret(t).Data[SecretKeyJWT]; ok {
		t.Error("jwt still in secret")
	}
}

func TestIdentityConflicts(t *testing.T) {
	e := setupIdentity(t)
	e.zc.Put(ziti.Identities, ziti.Entity{"name": "team-a.backend"})
	e.reconcile(t)
	if c := findCond(e.get(t), CondSynced); c.Reason != "NameConflict" {
		t.Errorf("synced = %+v", c)
	}
	if n := len(e.zc.Objects[ziti.Identities]); n != 1 {
		t.Errorf("identities = %d", n)
	}

	e = setupIdentity(t, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "team-a"}})
	e.reconcile(t)
	if c := findCond(e.get(t), CondSynced); c.Reason != "SecretConflict" {
		t.Errorf("synced = %+v", c)
	}
}

func findCond(z *zitiv1.ZitiIdentity, typ string) metav1.Condition {
	for _, c := range z.Status.Conditions {
		if c.Type == typ {
			return c
		}
	}
	return metav1.Condition{}
}

func TestIdentityDelete(t *testing.T) {
	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := setupIdentity(t)
		e.reconcile(t)
		z := e.get(t)
		z.Spec.DeletionPolicy = policy
		if err := e.k.Update(t.Context(), z); err != nil {
			t.Fatal(err)
		}
		if err := e.k.Delete(t.Context(), z); err != nil {
			t.Fatal(err)
		}
		e.reconcile(t)

		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
			for _, o := range e.zc.Objects[ziti.Identities] {
				if len(o.Tags()) != 0 {
					t.Errorf("orphaned identity keeps tags %v", o.Tags())
				}
			}
		}
		if n := len(e.zc.Objects[ziti.Identities]); n != want {
			t.Errorf("%s: identities = %d, want %d", policy, n, want)
		}
		if err := e.k.Get(t.Context(), e.key, &zitiv1.ZitiIdentity{}); !kerrors.IsNotFound(err) {
			t.Errorf("%s: CR still exists: %v", policy, err)
		}
	}
}

func operatorEnv(t *testing.T) *idEnv {
	t.Helper()
	e := setupIdentity(t)
	z := e.get(t)
	z.Spec.EnrollmentMode = zitiv1.EnrollmentOperator
	if err := e.k.Update(t.Context(), z); err != nil {
		t.Fatal(err)
	}
	e.r.Enroll = func(jwt string) ([]byte, error) { return []byte(`{"from":"` + jwt + `"}`), nil }
	return e
}

func TestOperatorEnrolledWritesIdentityFileOnly(t *testing.T) {
	e := operatorEnv(t)
	e.reconcile(t)

	z := e.get(t)
	s := e.secret(t)
	if len(s.Data) != 1 || len(s.Data[SecretKeyIdentity]) == 0 {
		t.Fatalf("secret keys = %v", s.Data)
	}
	if !z.Status.Enrolled || condStatusOf(z, CondReady) != metav1.ConditionTrue {
		t.Errorf("status = %+v", z.Status)
	}
	raw, _ := json.Marshal(z.Status)
	if strings.Contains(string(raw), "jwt-") {
		t.Error("jwt leaked into status")
	}
}

func TestOperatorEnrolledRecoversLostIdentityFile(t *testing.T) {
	e := operatorEnv(t)
	e.reconcile(t)
	e.identity()["authenticators"] = map[string]any{"cert": map[string]any{"id": "a1"}}
	first := e.identity().ID()
	if err := e.k.Delete(t.Context(), e.secret(t)); err != nil {
		t.Fatal(err)
	}

	e.reconcile(t)
	if got := e.get(t); got.Status.Enrolled || findCond(got, CondReady).Reason != "IdentityFileLost" {
		t.Fatalf("status = %+v", got.Status)
	}
	if len(e.zc.Objects[ziti.Identities]) != 0 {
		t.Fatal("identity not deleted")
	}

	e.reconcile(t)
	if e.identity().ID() == first || len(e.secret(t).Data[SecretKeyIdentity]) == 0 || !e.get(t).Status.Enrolled {
		t.Error("identity not enrolled again")
	}
}

func TestOperatorEnrollFailureIsReported(t *testing.T) {
	e := operatorEnv(t)
	e.r.Enroll = func(string) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err == nil {
		t.Fatal("want error")
	}
	if z := e.get(t); z.Status.Enrolled || findCond(z, CondReady).Reason != "Error" {
		t.Errorf("status = %+v", z.Status)
	}
}

func certPEM(t *testing.T, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: notAfter.Add(-2 * 365 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestIdentityCertificateExpiry(t *testing.T) {
	day := 24 * time.Hour
	for _, tc := range []struct {
		name      string
		notAfter  time.Duration
		certValid metav1.ConditionStatus
		reason    string
		ready     metav1.ConditionStatus
		warns     bool
	}{
		{"valid", 90 * day, metav1.ConditionTrue, "Valid", metav1.ConditionTrue, false},
		{"expires soon", 10 * day, metav1.ConditionFalse, "ExpiresSoon", metav1.ConditionTrue, true},
		{"expired", -day, metav1.ConditionFalse, "Expired", metav1.ConditionFalse, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupIdentity(t)
			e.reconcile(t)
			iden := e.identity()
			iden["authenticators"] = map[string]any{"cert": map[string]any{"id": "a1"}}
			e.zc.Put(ziti.Authenticators, ziti.Entity{"identity": iden.ID(), "method": "cert", "certPem": certPEM(t, time.Now().Add(tc.notAfter))})
			for len(e.rec.Events) > 0 {
				<-e.rec.Events
			}

			e.reconcile(t)
			z := e.get(t)
			if c := findCond(z, CondCertValid); c.Status != tc.certValid || c.Reason != tc.reason {
				t.Errorf("CertificateValid = %+v", c)
			}
			if condStatusOf(z, CondReady) != tc.ready {
				t.Errorf("Ready = %v", condStatusOf(z, CondReady))
			}
			if z.Status.CertNotAfter == nil || z.Status.CertNotAfter.Time.Before(time.Now().Add(tc.notAfter-time.Minute)) {
				t.Errorf("certNotAfter = %v", z.Status.CertNotAfter)
			}
			if got := len(e.rec.Events) > 0; got != tc.warns {
				t.Errorf("warning event = %v, want %v", got, tc.warns)
			}
			for len(e.rec.Events) > 0 {
				<-e.rec.Events
			}
			e.reconcile(t)
			if len(e.rec.Events) != 0 {
				t.Error("warning repeated on the next reconcile")
			}
		})
	}
}

func handMade(e *idEnv, tags map[string]any) string {
	return e.zc.Put(ziti.Identities, ziti.Entity{
		"name": "team-a.backend", "type": "Default", "roleAttributes": []string{"old"}, "authPolicyId": "default",
		"externalId": "ext-1", "tags": tags, "authenticators": map[string]any{"cert": map[string]any{"id": "a1"}},
	})
}

func setPolicy(t *testing.T, e *idEnv, p zitiv1.ManagementPolicy) {
	t.Helper()
	z := e.get(t)
	z.Spec.ManagementPolicy = p
	if err := e.k.Update(t.Context(), z); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityAdoptKeepsTagsAndReleasesInsteadOfDeleting(t *testing.T) {
	e := setupIdentity(t)
	setPolicy(t, e, zitiv1.ManagementAdopt)
	id := handMade(e, map[string]any{"owner": "human"})
	e.reconcile(t)

	got := e.zc.Objects[ziti.Identities][id]
	tags := got.Tags()
	if tags["owner"] != "human" || tags[desired.TagUID] != "uid-1" || tags[desired.TagAdopted] != "true" {
		t.Fatalf("tags = %v", tags)
	}
	if attrs, _ := json.Marshal(got["roleAttributes"]); string(attrs) != `["team-a.web"]` || got["externalId"] != "ext-1" {
		t.Errorf("identity = %v", got)
	}
	for _, c := range e.zc.Calls {
		if strings.HasPrefix(c, "update") || strings.HasPrefix(c, "create identities") || strings.HasPrefix(c, "delete") {
			t.Errorf("unexpected write %q", c)
		}
	}
	z := e.get(t)
	if z.Status.ZitiID != id || !z.Status.Enrolled {
		t.Errorf("status = %+v", z.Status)
	}
	if _, err := e.secretOrNil(t); err == nil {
		t.Error("adopting an enrolled identity must not create a Secret")
	}

	if err := e.k.Delete(t.Context(), z); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t)
	got = e.zc.Objects[ziti.Identities][id]
	if got == nil {
		t.Fatal("adopted identity was deleted")
	}
	if tags := got.Tags(); tags["owner"] != "human" || len(tags) != 1 {
		t.Errorf("tags after release = %v", tags)
	}
}

func (e *idEnv) secretOrNil(t *testing.T) (*corev1.Secret, error) {
	var s corev1.Secret
	err := e.k.Get(t.Context(), e.key, &s)
	return &s, err
}

func TestIdentityAdoptErrors(t *testing.T) {
	e := setupIdentity(t)
	setPolicy(t, e, zitiv1.ManagementAdopt)
	e.reconcile(t)
	if c := findCond(e.get(t), CondSynced); c.Reason != "NotFound" {
		t.Errorf("missing identity: %+v", c)
	}
	if len(e.zc.Objects[ziti.Identities]) != 0 {
		t.Error("adopt must not create")
	}

	handMade(e, map[string]any{desired.TagUID: "other"})
	e.reconcile(t)
	if c := findCond(e.get(t), CondSynced); c.Reason != "NameConflict" {
		t.Errorf("owned by another: %+v", c)
	}
}

func TestIdentityObserveNeverWrites(t *testing.T) {
	e := setupIdentity(t)
	setPolicy(t, e, zitiv1.ManagementObserve)
	id := handMade(e, map[string]any{"owner": "human"})
	e.reconcile(t)

	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			t.Errorf("unexpected write %q", c)
		}
	}
	z := e.get(t)
	if z.Status.ZitiID != id || !z.Status.Enrolled || condStatusOf(z, CondReady) != metav1.ConditionTrue {
		t.Errorf("status = %+v", z.Status)
	}
	if len(z.Finalizers) != 0 {
		t.Errorf("finalizers = %v", z.Finalizers)
	}
	if _, err := e.secretOrNil(t); err == nil {
		t.Error("observe must not create a Secret")
	}
	if got := e.zc.Objects[ziti.Identities][id]; got.Tags()["owner"] != "human" || len(got.Tags()) != 1 {
		t.Errorf("identity changed: %v", got)
	}
}

func TestOperatorRenewsCertificateAutomatically(t *testing.T) {
	e := operatorEnv(t)
	e.reconcile(t)
	iden := e.identity()
	iden["authenticators"] = map[string]any{"cert": map[string]any{"id": "a1"}}
	e.zc.Put(ziti.Authenticators, ziti.Entity{"identity": iden.ID(), "method": "cert", "certPem": certPEM(t, time.Now().Add(10*24*time.Hour))})

	// identity.json in the Secret holds a certificate that expires in 10 days
	old := identityJSONWithCert(t, time.Now().Add(-355*24*time.Hour), time.Now().Add(10*24*time.Hour))
	s := e.secret(t)
	s.Data[SecretKeyIdentity] = old
	if err := e.k.Update(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	e.r.Extend = func(file []byte, authID string) ([]byte, error) {
		gotAuth = authID
		return identityJSONWithCert(t, time.Now(), time.Now().Add(365*24*time.Hour)), nil
	}

	e.reconcile(t)
	if gotAuth == "" {
		t.Fatal("certificate was not renewed")
	}
	_, na, err := ziti.CertValidity(e.secret(t).Data[SecretKeyIdentity])
	if err != nil || time.Until(na) < 300*24*time.Hour {
		t.Errorf("secret holds the old certificate: %v %v", na, err)
	}

	gotAuth = ""
	e.reconcile(t)
	if gotAuth != "" {
		t.Error("renewed again while the certificate is fresh")
	}
}

func TestOperatorRenewalFailureKeepsFileAndRejectedCertRecreates(t *testing.T) {
	e := operatorEnv(t)
	e.reconcile(t)
	iden := e.identity()
	first := iden.ID()
	iden["authenticators"] = map[string]any{"cert": map[string]any{"id": "a1"}}
	e.zc.Put(ziti.Authenticators, ziti.Entity{"identity": first, "method": "cert", "certPem": certPEM(t, time.Now().Add(10*24*time.Hour))})
	old := identityJSONWithCert(t, time.Now().Add(-355*24*time.Hour), time.Now().Add(10*24*time.Hour))
	s := e.secret(t)
	s.Data[SecretKeyIdentity] = old
	if err := e.k.Update(t.Context(), s); err != nil {
		t.Fatal(err)
	}

	e.r.Extend = func([]byte, string) ([]byte, error) { return nil, errors.New("controller down") }
	e.reconcile(t)
	if string(e.secret(t).Data[SecretKeyIdentity]) != string(old) {
		t.Error("failed renewal changed the Secret")
	}
	if z := e.get(t); !z.Status.Enrolled || findCond(z, CondCertValid).Reason != "ExpiresSoon" {
		t.Errorf("status = %+v", z.Status)
	}

	e.r.Extend = func([]byte, string) ([]byte, error) { return nil, &ziti.APIError{Status: 401} }
	e.reconcile(t)
	if len(e.zc.Objects[ziti.Identities]) != 0 || findCond(e.get(t), CondReady).Reason != "IdentityFileLost" {
		t.Fatalf("rejected certificate did not trigger recreate: %+v", e.get(t).Status)
	}
}

func identityJSONWithCert(t *testing.T, notBefore, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	out, _ := json.Marshal(map[string]any{"ztAPI": "https://ctrl", "id": map[string]string{
		"cert": "pem:" + string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		"key":  "pem:" + string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
	}})
	return out
}

func TestIdentitySecretIsLabeledAndHiddenSecretIsAConflict(t *testing.T) {
	e := setupIdentity(t)
	e.reconcile(t)
	if got := e.secret(t).Labels[ManagedByLabel]; got != ManagedByLabelValue {
		t.Errorf("secret label = %q", got)
	}

	// A filtered cache cannot see an unlabeled Secret: Get says not found, Create says already exists.
	hidden := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "team-a"}}
	e2 := setupIdentity(t, hidden)
	e2.r.Client = hideSecrets{e2.r.Client}
	e2.reconcile(t)
	if c := findCond(e2.get(t), CondSynced); c.Reason != "SecretConflict" {
		t.Errorf("synced = %+v", c)
	}
}

// hideSecrets behaves like a cache that filters out Secrets without the operator label.
type hideSecrets struct{ client.Client }

func (h hideSecrets) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return kerrors.NewNotFound(corev1.Resource("secrets"), key.Name)
	}
	return h.Client.Get(ctx, key, obj, opts...)
}
