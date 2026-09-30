/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const defaultTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// signerKinds lists the Ziti kinds a signer owns. Auth policies come first so they go before the signer on delete.
var signerKinds = []ziti.Kind{ziti.AuthPolicies, ziti.ExternalJWTSigners}

type ZitiJwtSignerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// Keys reads the cluster issuer and keys for keys.kubernetes.
	Keys ClusterKeys
	// TokenFile is the operator's own service account token. Its kid names the key the cluster signs with.
	TokenFile string
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitijwtsigners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitijwtsigners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitijwtsigners/finalizers,verbs=update

func (r *ZitiJwtSignerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sg zitiv1alpha1.ZitiJwtSigner
	if err := r.Get(ctx, req.NamespacedName, &sg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !sg.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &sg)
	}
	if controllerutil.AddFinalizer(&sg, Finalizer) {
		if err := r.Update(ctx, &sg); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := sg.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, sg.Spec.ConnectionRef)
	if err == nil {
		err = r.sync(ctx, &sg, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &se):
		markFailed(&sg.Status.Conditions, sg.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&sg.Status.Conditions, sg.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&sg.Status.Conditions, sg.Generation, "Error", err.Error())
	}
	sg.Status.ObservedGeneration = sg.Generation

	if !equality.Semantic.DeepEqual(before, &sg.Status) {
		if uerr := r.Status().Update(ctx, &sg); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiJwtSignerReconciler) finalize(ctx context.Context, sg *zitiv1alpha1.ZitiJwtSigner) error {
	if !controllerutil.ContainsFinalizer(sg, Finalizer) {
		return nil
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, sg.Spec.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(sg, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, sg, sg.UID, signerKinds)
	if err != nil {
		return err
	}
	if sg.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(sg, Finalizer)
	return r.Update(ctx, sg)
}

// keyMaterial returns the issuer, the key id, and the certificate that carry the signing key.
// With jwksEndpoint the Ziti controller fetches the keys itself and the last two are empty.
func (r *ZitiJwtSignerReconciler) keyMaterial(ctx context.Context, sg *zitiv1alpha1.ZitiJwtSigner) (issuer, kid, certPEM string, err error) {
	if ep := sg.Spec.Keys.JwksEndpoint; ep != nil {
		return ep.Issuer, "", "", nil
	}
	if r.Keys == nil {
		return "", "", "", &specError{"InvalidSpec", "keys.kubernetes needs the operator to run in the cluster"}
	}
	issuer, raw, err := r.Keys.Fetch(ctx)
	if err != nil {
		return "", "", "", err
	}
	keys, err := ziti.ParseJWKS(raw)
	if err != nil {
		return "", "", "", &specError{"InvalidSpec", err.Error()}
	}
	key, err := pickKey(keys, r.currentKid())
	if err != nil {
		return "", "", "", &specError{"KeyAmbiguous", err.Error()}
	}
	certPEM, err = ziti.KeyCertificate(key.Kid, key.Key)
	return issuer, key.Kid, certPEM, err
}

func (r *ZitiJwtSignerReconciler) currentKid() string {
	path := r.TokenFile
	if path == "" {
		path = defaultTokenFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return ziti.TokenKid(strings.TrimSpace(string(raw)))
}

// pickKey chooses the key that Kubernetes signs with. A cluster with several keys is rotating,
// and the header of the operator's own token names the current one.
func pickKey(keys []ziti.JWK, currentKid string) (ziti.JWK, error) {
	if len(keys) == 1 {
		return keys[0], nil
	}
	for _, k := range keys {
		if k.Kid == currentKid {
			return k, nil
		}
	}
	return ziti.JWK{}, fmt.Errorf("the cluster publishes %d signing keys and none matches the operator's own token, so the current key is unknown", len(keys))
}

func (r *ZitiJwtSignerReconciler) sync(ctx context.Context, sg *zitiv1alpha1.ZitiJwtSigner, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.SignerName(sg)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}
	issuer, kid, certPEM, err := r.keyMaterial(ctx, sg)
	if err != nil {
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, sg, sg.UID, signerKinds)
	if err != nil {
		return err
	}
	signerID, err := set.ensure(ctx, ziti.ExternalJWTSigners, desired.JwtSigner(sg, conn, issuer, kid, certPEM))
	if err != nil {
		return err
	}
	var policyID string
	if desired.SignerWantsPolicy(sg) {
		if policyID, err = set.ensure(ctx, ziti.AuthPolicies, desired.SignerAuthPolicy(sg, conn, signerID)); err != nil {
			return err
		}
	}
	if err := set.prune(ctx); err != nil {
		return err
	}
	sg.Status.SignerID, sg.Status.AuthPolicyID, sg.Status.Issuer, sg.Status.KeyID = signerID, policyID, issuer, kid
	setCond(&sg.Status.Conditions, sg.Generation, CondSynced, true, "Synced", "", "")
	setCond(&sg.Status.Conditions, sg.Generation, CondReady, true, "Ready", "", "")
	return nil
}

func (r *ZitiJwtSignerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiJwtSigner{}).
		Named("zitijwtsigner").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
