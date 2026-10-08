// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type stubKeys struct {
	issuer string
	jwks   []byte
	err    error
}

func (s *stubKeys) Fetch(context.Context) (string, []byte, error) { return s.issuer, s.jwks, s.err }

type jwksSet struct {
	raw  []byte
	kids []string
}

func makeJWKS(t *testing.T, kids ...string) jwksSet {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	var keys []map[string]any
	for _, kid := range kids {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, map[string]any{"kid": kid, "kty": "RSA", "n": enc(k.N.Bytes()), "e": enc([]byte{1, 0, 1})})
	}
	raw, _ := json.Marshal(map[string]any{"keys": keys})
	return jwksSet{raw: raw, kids: kids}
}

type signerEnv struct {
	r    *ZitiJwtSignerReconciler
	zc   *ziti.Fake
	k    client.Client
	keys *stubKeys
	key  types.NamespacedName
}

func setupSigner(t *testing.T, mut func(*zitiv1.ZitiJwtSigner), set jwksSet, tokenKid string) *signerEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	conn := &zitiv1.ZitiConnection{Name: "default"}
	sg := &zitiv1.ZitiJwtSigner{
		Name: "k8s", UID: "uid-sg", Generation: 1,
		Spec: zitiv1.ZitiJwtSignerSpec{
			ConnectionRef: "default", Audience: "ziti", DeletionPolicy: zitiv1.DeletionPolicyDelete,
			Keys: zitiv1.SignerKeys{Kubernetes: &zitiv1.KubernetesKeys{}},
		},
	}
	if mut != nil {
		mut(sg)
	}
	k := fake.NewClientBuilder().WithScheme(scheme).WithObjects(conn, sg).WithStatusSubresource(&zitiv1.ZitiJwtSigner{}).Build()
	zc := ziti.NewFake()
	keys := &stubKeys{issuer: "https://kube.example", jwks: set.raw}

	tokenFile := ""
	if tokenKid != "" {
		head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"` + tokenKid + `"}`))
		tokenFile = filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(tokenFile, []byte(head+".payload.sig\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &signerEnv{
		r:  &ZitiJwtSignerReconciler{Client: k, Scheme: scheme, Clients: staticProvider{zc}, Recorder: record.NewFakeRecorder(50), Keys: keys, TokenFile: tokenFile},
		zc: zc, k: k, keys: keys, key: types.NamespacedName{Name: "k8s"},
	}
}

func (e *signerEnv) reconcile(t *testing.T) *zitiv1.ZitiJwtSigner {
	t.Helper()
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var sg zitiv1.ZitiJwtSigner
	if err := e.k.Get(t.Context(), e.key, &sg); err != nil {
		return nil
	}
	return &sg
}

func (e *signerEnv) writes() []string {
	var w []string
	for _, c := range e.zc.Calls {
		if !strings.HasPrefix(c, "list") {
			w = append(w, c)
		}
	}
	return w
}

func only(objs map[string]ziti.Entity) ziti.Entity {
	for _, o := range objs {
		return o
	}
	return nil
}

func TestSignerFromKubernetesCreatesSignerAndPolicy(t *testing.T) {
	e := setupSigner(t, nil, makeJWKS(t, "key-1"), "")
	sg := e.reconcile(t)

	signer, policy := only(e.zc.Objects[ziti.ExternalJWTSigners]), only(e.zc.Objects[ziti.AuthPolicies])
	if signer.Name() != "k8s" || signer["issuer"] != "https://kube.example" || signer["audience"] != "ziti" ||
		signer["kid"] != "key-1" || signer["claimsProperty"] != "sub" || signer["useExternalId"] != true {
		t.Errorf("signer = %v", signer)
	}
	if !strings.Contains(signer["certPem"].(string), "BEGIN CERTIFICATE") {
		t.Error("the key must reach Ziti as a certificate")
	}
	ext := policy["primary"].(map[string]any)["extJwt"].(map[string]any)
	if policy.Name() != "k8s" || ext["allowed"] != true || len(ext["allowedSigners"].([]any)) != 1 || ext["allowedSigners"].([]any)[0] != signer.ID() {
		t.Errorf("policy = %v", policy)
	}
	if sg.Status.SignerID != signer.ID() || sg.Status.AuthPolicyID != policy.ID() || sg.Status.KeyID != "key-1" || sg.Status.Issuer != "https://kube.example" {
		t.Errorf("status = %+v", sg.Status)
	}
	if condStatusOfList(sg.Status.Conditions, CondReady) != metav1.ConditionTrue {
		t.Errorf("conditions = %+v", sg.Status.Conditions)
	}

	e.zc.Calls = nil
	e.reconcile(t)
	if w := e.writes(); len(w) != 0 {
		t.Errorf("second reconcile wrote: %v", w)
	}
}

func TestSignerFollowsKeyRotation(t *testing.T) {
	first := makeJWKS(t, "old", "new")
	e := setupSigner(t, nil, first, "old")
	e.reconcile(t)
	if got := only(e.zc.Objects[ziti.ExternalJWTSigners])["kid"]; got != "old" {
		t.Fatalf("kid = %v, the operator's own token names the old key", got)
	}
	oldCert := only(e.zc.Objects[ziti.ExternalJWTSigners])["certPem"]

	head := base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"new"}`))
	if err := os.WriteFile(e.r.TokenFile, []byte(head+".p.s"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.zc.Calls = nil
	e.reconcile(t)
	signer := only(e.zc.Objects[ziti.ExternalJWTSigners])
	if signer["kid"] != "new" || signer["certPem"] == oldCert {
		t.Errorf("signer did not follow the new key: %v", signer["kid"])
	}
	if len(e.zc.Objects[ziti.ExternalJWTSigners]) != 1 || len(e.writes()) != 1 {
		t.Errorf("rotation must update the one signer in place: %v", e.writes())
	}
}

func TestSignerRefusesToGuessTheKey(t *testing.T) {
	e := setupSigner(t, nil, makeJWKS(t, "a", "b"), "")
	sg := e.reconcile(t)
	if c := findSignerCond(sg); c.Reason != "KeyAmbiguous" {
		t.Errorf("synced = %+v", c)
	}
	if len(e.zc.Objects[ziti.ExternalJWTSigners]) != 0 {
		t.Error("nothing may be created when the key is unknown")
	}
	e2 := setupSigner(t, nil, makeJWKS(t, "a", "b"), "not-in-the-set")
	if c := findSignerCond(e2.reconcile(t)); c.Reason != "KeyAmbiguous" {
		t.Errorf("token kid outside the JWKS: %+v", c)
	}
}

func findSignerCond(sg *zitiv1.ZitiJwtSigner) metav1.Condition {
	for _, c := range sg.Status.Conditions {
		if c.Type == CondSynced {
			return c
		}
	}
	return metav1.Condition{}
}

func TestSignerWithJwksEndpointNeedsNoClusterAccess(t *testing.T) {
	e := setupSigner(t, func(sg *zitiv1.ZitiJwtSigner) {
		sg.Spec.Keys = zitiv1.SignerKeys{JwksEndpoint: &zitiv1.JwksEndpoint{URL: "https://idp.example/keys", Issuer: "https://idp.example"}}
		sg.Spec.CreateAuthPolicy = new(bool)
		sg.Spec.ZitiName = "idp"
	}, jwksSet{}, "")
	e.r.Keys = nil
	sg := e.reconcile(t)
	signer := only(e.zc.Objects[ziti.ExternalJWTSigners])
	if signer.Name() != "idp" || signer["jwksEndpoint"] != "https://idp.example/keys" || signer["issuer"] != "https://idp.example" {
		t.Errorf("signer = %v", signer)
	}
	if _, has := signer["certPem"]; has {
		t.Error("jwksEndpoint must not carry a certificate")
	}
	if len(e.zc.Objects[ziti.AuthPolicies]) != 0 || sg.Status.AuthPolicyID != "" || sg.Status.KeyID != "" {
		t.Errorf("createAuthPolicy false: policies=%d status=%+v", len(e.zc.Objects[ziti.AuthPolicies]), sg.Status)
	}
}

func TestSignerErrorsAreReported(t *testing.T) {
	e := setupSigner(t, nil, jwksSet{raw: []byte(`{"keys":[]}`)}, "")
	if c := findSignerCond(e.reconcile(t)); c.Reason != "InvalidSpec" {
		t.Errorf("empty JWKS: %+v", c)
	}
	e = setupSigner(t, nil, makeJWKS(t, "k"), "")
	e.keys.err = errors.New("apiserver down")
	if _, err := e.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: e.key}); err == nil {
		t.Error("a fetch failure must be retried")
	}
	e = setupSigner(t, nil, makeJWKS(t, "k"), "")
	e.r.Keys = nil
	if c := findSignerCond(e.reconcile(t)); c.Reason != "InvalidSpec" || !strings.Contains(c.Message, "in the cluster") {
		t.Errorf("no cluster access: %+v", c)
	}
}

func TestSignerNameConflictAndDelete(t *testing.T) {
	e := setupSigner(t, nil, makeJWKS(t, "k"), "")
	e.zc.Put(ziti.ExternalJWTSigners, ziti.Entity{"name": "k8s"})
	if c := findSignerCond(e.reconcile(t)); c.Reason != "NameConflict" {
		t.Errorf("synced = %+v", c)
	}

	for _, policy := range []zitiv1.DeletionPolicy{zitiv1.DeletionPolicyDelete, zitiv1.DeletionPolicyOrphan} {
		e := setupSigner(t, func(sg *zitiv1.ZitiJwtSigner) { sg.Spec.DeletionPolicy = policy }, makeJWKS(t, "k"), "")
		sg := e.reconcile(t)
		if err := e.k.Delete(t.Context(), sg); err != nil {
			t.Fatal(err)
		}
		e.reconcile(t)
		want := 0
		if policy == zitiv1.DeletionPolicyOrphan {
			want = 1
		}
		if n := len(e.zc.Objects[ziti.ExternalJWTSigners]); n != want {
			t.Errorf("%s: signers = %d, want %d", policy, n, want)
		}
		for _, o := range e.zc.Objects[ziti.ExternalJWTSigners] {
			if len(o.Tags()) != 0 {
				t.Errorf("released signer keeps tags %v", o.Tags())
			}
		}
	}
}
