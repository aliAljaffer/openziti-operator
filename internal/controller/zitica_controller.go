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
	"crypto/sha1"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const CondVerified = "Verified"

var caKinds = []ziti.Kind{ziti.CertificateAuthorities}

type ZitiCAReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// Reader reads Secrets without the cache. The cache holds only the Secrets that the operator creates.
	Reader client.Reader
}

// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=ziticas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=ziticas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=ziticas/finalizers,verbs=update

func (r *ZitiCAReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ca zitiv1alpha1.ZitiCA
	if err := r.Get(ctx, req.NamespacedName, &ca); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ca.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &ca)
	}
	if controllerutil.AddFinalizer(&ca, Finalizer) {
		if err := r.Update(ctx, &ca); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := ca.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, ca.Spec.ConnectionRef)
	if err == nil {
		err = r.sync(ctx, &ca, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil && ca.Status.Verified:
		next.RequeueAfter = jitter(serviceResync)
	case err == nil:
		next.RequeueAfter = jitter(pendingRecheck)
	case errors.As(err, &se):
		markFailed(&ca.Status.Conditions, ca.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&ca.Status.Conditions, ca.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&ca.Status.Conditions, ca.Generation, "Error", err.Error())
	}
	ca.Status.ObservedGeneration = ca.Generation

	if !equality.Semantic.DeepEqual(before, &ca.Status) {
		if uerr := r.Status().Update(ctx, &ca); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiCAReconciler) finalize(ctx context.Context, ca *zitiv1alpha1.ZitiCA) error {
	if !controllerutil.ContainsFinalizer(ca, Finalizer) {
		return nil
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, ca.Spec.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(ca, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, ca, ca.UID, caKinds)
	if err != nil {
		return err
	}
	if ca.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(ca, Finalizer)
	return r.Update(ctx, ca)
}

func (r *ZitiCAReconciler) readSecret(ctx context.Context, ref zitiv1alpha1.SecretRef) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &secret); err != nil {
		return nil, fmt.Errorf("Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	return &secret, nil
}

func (r *ZitiCAReconciler) sync(ctx context.Context, ca *zitiv1alpha1.ZitiCA, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.CAName(ca)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}
	ref := ca.Spec.Certificate.SecretRef
	secret, err := r.readSecret(ctx, ref)
	if err != nil {
		return err
	}
	certKey := ca.Spec.Certificate.CertKey
	if certKey == "" {
		certKey = corev1.TLSCertKey
	}
	caCert, certPEM, err := ziti.FirstCertificate(secret.Data[certKey])
	if err != nil {
		return &specError{"InvalidSpec", fmt.Sprintf("Secret %s/%s key %q: %v", ref.Namespace, ref.Name, certKey, err)}
	}
	if !caCert.IsCA {
		return &specError{"InvalidSpec", fmt.Sprintf("the certificate in Secret %s/%s is not a CA certificate", ref.Namespace, ref.Name)}
	}

	set, err := newEntitySet(ctx, zc, r.Recorder, ca, ca.UID, caKinds)
	if err != nil {
		return err
	}
	// Ziti ignores a new certPem on an existing CA. A renewed issuer needs a new CA entity, verified again.
	if old, ok := set.existing[ziti.CertificateAuthorities][name]; ok {
		if fp, _ := old["fingerprint"].(string); !strings.EqualFold(fp, fmt.Sprintf("%x", sha1.Sum(caCert.Raw))) {
			if err := zc.Delete(ctx, ziti.CertificateAuthorities, old.ID()); err != nil {
				return err
			}
			delete(set.existing[ziti.CertificateAuthorities], name)
			r.Recorder.Eventf(ca, "Normal", "Renewed", "the CA certificate changed, replaced CA %s in Ziti", name)
		}
	}
	caID, err := set.ensure(ctx, ziti.CertificateAuthorities, desired.CA(ca, conn, certPEM))
	if err != nil {
		return err
	}
	if err := set.prune(ctx); err != nil {
		return err
	}
	setCond(&ca.Status.Conditions, ca.Generation, CondSynced, true, "Synced", "", "")

	entity, err := r.fetch(ctx, zc, ca.UID)
	if err != nil {
		return err
	}
	verifyErr := error(nil)
	if verified, _ := entity["isVerified"].(bool); !verified && ca.Spec.Verification.SignWithSecretKey {
		if verifyErr = r.verify(ctx, zc, entity, caCert, secret); verifyErr == nil {
			r.Recorder.Eventf(ca, "Normal", "Verified", "proved ownership of CA %s to Ziti", name)
			if entity, err = r.fetch(ctx, zc, ca.UID); err != nil {
				return err
			}
		} else if !ziti.IsSpecError(verifyErr) && !isVerifySpecError(verifyErr) {
			return verifyErr
		}
	}

	verified, _ := entity["isVerified"].(bool)
	token, _ := entity["verificationToken"].(string)
	fingerprint, _ := entity["fingerprint"].(string)
	ca.Status.CAID, ca.Status.Fingerprint, ca.Status.Verified, ca.Status.VerificationToken = caID, fingerprint, verified, token
	if verified {
		ca.Status.VerificationToken = ""
		setCond(&ca.Status.Conditions, ca.Generation, CondVerified, true, "Verified", "", "")
		setCond(&ca.Status.Conditions, ca.Generation, CondReady, true, "Ready", "", "")
		return nil
	}
	reason, msg := "AwaitingVerification", fmt.Sprintf("sign a certificate with common name %q using the CA key and send it to Ziti, or set verification.signWithSecretKey", token)
	if verifyErr != nil {
		reason, msg = "VerificationFailed", verifyErr.Error()
	}
	setCond(&ca.Status.Conditions, ca.Generation, CondVerified, false, "", reason, msg)
	setCond(&ca.Status.Conditions, ca.Generation, CondReady, false, "", reason, msg)
	return nil
}

func isVerifySpecError(err error) bool {
	var v *verifyError
	return errors.As(err, &v)
}

type verifyError struct{ msg string }

func (e *verifyError) Error() string { return e.msg }

func (r *ZitiCAReconciler) fetch(ctx context.Context, zc ziti.Client, uid types.UID) (ziti.Entity, error) {
	list, err := zc.List(ctx, ziti.CertificateAuthorities, tagFilter(uid))
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("the CA is not in Ziti yet")
	}
	return list[0], nil
}

// verify signs the one-time proof with the CA key of the Secret. The key stays in memory.
func (r *ZitiCAReconciler) verify(ctx context.Context, zc ziti.Client, entity ziti.Entity, caCert *x509.Certificate, secret *corev1.Secret) error {
	token, _ := entity["verificationToken"].(string)
	if token == "" {
		return &verifyError{"Ziti gave no verification token"}
	}
	keyPEM := secret.Data[corev1.TLSPrivateKeyKey]
	if len(keyPEM) == 0 {
		return &verifyError{fmt.Sprintf("Secret %s/%s has no %s key", secret.Namespace, secret.Name, corev1.TLSPrivateKeyKey)}
	}
	key, err := ziti.ParsePrivateKey(keyPEM)
	if err != nil {
		return &verifyError{"CA private key: " + err.Error()}
	}
	proof, err := ziti.ProofCertificate(token, caCert, key)
	if err != nil {
		return &verifyError{"sign the proof: " + err.Error()}
	}
	return zc.Verify(ctx, ziti.CertificateAuthorities, entity.ID(), proof)
}

func (r *ZitiCAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiCA{}).
		Named("zitica").
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
