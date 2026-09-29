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
	"maps"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	SecretKeyJWT      = "enrollment.jwt"
	SecretKeyIdentity = "identity.json"
	defaultAuthPolicy = "Default"
	enrollmentTTL     = 24 * time.Hour
	pendingRecheck    = time.Minute
)

type ZitiIdentityReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// Enroll turns an enrollment JWT into identity.json. Nil uses ziti.EnrollOTT.
	Enroll func(jwt string) ([]byte, error)
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *ZitiIdentityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var id zitiv1alpha1.ZitiIdentity
	if err := r.Get(ctx, req.NamespacedName, &id); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !id.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &id)
	}
	if controllerutil.AddFinalizer(&id, Finalizer) {
		if err := r.Update(ctx, &id); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := id.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, id.Spec.ConnectionRef)
	if err == nil {
		err = r.sync(ctx, &id, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil && id.Status.Enrolled:
		next.RequeueAfter = jitter(serviceResync)
	case err == nil:
		next.RequeueAfter = jitter(pendingRecheck)
	case errors.As(err, &se):
		markIdentityFailed(&id, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markIdentityFailed(&id, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markIdentityFailed(&id, "Error", err.Error())
	}
	id.Status.ObservedGeneration = id.Generation

	if !equality.Semantic.DeepEqual(before, &id.Status) {
		if uerr := r.Status().Update(ctx, &id); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiIdentityReconciler) finalize(ctx context.Context, id *zitiv1alpha1.ZitiIdentity) error {
	if !controllerutil.ContainsFinalizer(id, Finalizer) {
		return nil
	}
	if id.Spec.DeletionPolicy != zitiv1alpha1.DeletionPolicyOrphan {
		_, zc, err := connect(ctx, r.Client, r.Clients, id.Spec.ConnectionRef)
		if err != nil {
			r.Recorder.Eventf(id, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
			return err
		}
		existing, err := zc.List(ctx, ziti.Identities, tagFilter(id.UID))
		if err != nil {
			return err
		}
		for _, e := range existing {
			if err := zc.Delete(ctx, ziti.Identities, e.ID()); err != nil {
				return err
			}
			r.Recorder.Eventf(id, "Normal", "Deleted", "deleted identity %s", e.Name())
		}
	}
	controllerutil.RemoveFinalizer(id, Finalizer)
	return r.Update(ctx, id)
}

func (r *ZitiIdentityReconciler) sync(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.IdentityName(id)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}

	policyName := id.Spec.AuthPolicy
	if policyName == "" {
		policyName = defaultAuthPolicy
	}
	policies, err := zc.List(ctx, ziti.AuthPolicies, fmt.Sprintf(`name="%s"`, policyName))
	if err != nil {
		return err
	}
	if len(policies) != 1 {
		return &specError{"InvalidSpec", fmt.Sprintf("auth policy %q not found in Ziti", policyName)}
	}
	body, err := desired.Identity(id, conn, policies[0].ID())
	if err != nil {
		return &specError{"InvalidSpec", err.Error()}
	}

	existing, err := zc.List(ctx, ziti.Identities, tagFilter(id.UID))
	if err != nil {
		return err
	}
	var zid ziti.Entity
	switch {
	case len(existing) > 0:
		zid = existing[0]
		if !desired.Matches(body, zid) {
			if err := zc.Update(ctx, ziti.Identities, zid.ID(), body); err != nil {
				return err
			}
			r.Recorder.Eventf(id, "Normal", "Updated", "updated identity %s", name)
		}
	default:
		clash, err := zc.List(ctx, ziti.Identities, fmt.Sprintf(`name="%s"`, name))
		if err != nil {
			return err
		}
		if len(clash) > 0 {
			return &specError{"NameConflict", fmt.Sprintf("identity %q already exists and is not managed by this operator", name)}
		}
		create := ziti.Entity{"enrollment": map[string]any{"ott": true}}
		maps.Copy(create, body)
		newID, err := zc.Create(ctx, ziti.Identities, create)
		if err != nil {
			return err
		}
		r.Recorder.Eventf(id, "Normal", "Created", "created identity %s", name)
		zid = ziti.Entity{"id": newID}
	}

	id.Status.ZitiID = zid.ID()
	setIdentityCond(id, CondSynced, true, "Synced", "", "")

	// The identity list shows a non-empty authenticators map after enrollment.
	auth, _ := zid["authenticators"].(map[string]any)
	id.Status.Enrolled = len(auth) > 0
	operator := id.Spec.EnrollmentMode == zitiv1alpha1.EnrollmentOperator

	switch {
	case id.Status.Enrolled && operator:
		var secret corev1.Secret
		err := r.Get(ctx, r.secretKey(id), &secret)
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		if len(secret.Data[SecretKeyIdentity]) > 0 {
			id.Status.EnrollmentExpiresAt = nil
			setIdentityCond(id, CondReady, true, "Enrolled", "", "")
			return nil
		}
		// The private key exists only in identity.json. Without it the identity is unusable, so start over.
		if err := zc.Delete(ctx, ziti.Identities, zid.ID()); err != nil {
			return err
		}
		r.Recorder.Eventf(id, "Warning", "IdentityFileLost", "identity is enrolled but Secret has no %s, deleted identity to enroll again", SecretKeyIdentity)
		id.Status.ZitiID, id.Status.Enrolled = "", false
		setIdentityCond(id, CondReady, false, "", "IdentityFileLost", "identity file was lost, enrolling again")
		return nil
	case id.Status.Enrolled:
		id.Status.EnrollmentExpiresAt = nil
		setIdentityCond(id, CondReady, true, "Enrolled", "", "")
		return r.dropJWT(ctx, id)
	}

	setIdentityCond(id, CondReady, false, "", "PendingEnrollment", "identity is not enrolled yet")
	jwt, err := r.currentEnrollmentJWT(ctx, id, zc)
	if err != nil {
		return err
	}
	if !operator {
		return r.writeSecret(ctx, id, SecretKeyJWT, []byte(jwt))
	}
	enroll := r.Enroll
	if enroll == nil {
		enroll = ziti.EnrollOTT
	}
	file, err := enroll(jwt)
	if err != nil {
		return fmt.Errorf("enroll identity: %w", err)
	}
	if err := r.writeSecret(ctx, id, SecretKeyIdentity, file); err != nil {
		return err
	}
	r.Recorder.Eventf(id, "Normal", "Enrolled", "enrolled identity %s", name)
	id.Status.Enrolled, id.Status.EnrollmentExpiresAt = true, nil
	setIdentityCond(id, CondReady, true, "Enrolled", "", "")
	return nil
}

func (r *ZitiIdentityReconciler) secretKey(id *zitiv1alpha1.ZitiIdentity) types.NamespacedName {
	n := id.Spec.SecretName
	if n == "" {
		n = id.Name
	}
	return types.NamespacedName{Namespace: id.Namespace, Name: n}
}

// currentEnrollmentJWT returns the JWT of a valid enrollment. It replaces expired ones.
func (r *ZitiIdentityReconciler) currentEnrollmentJWT(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, zc ziti.Client) (string, error) {
	enrollments, err := zc.List(ctx, ziti.Enrollments, fmt.Sprintf(`identity="%s"`, id.Status.ZitiID))
	if err != nil {
		return "", err
	}
	var current ziti.Entity
	for _, e := range enrollments {
		exp, _ := time.Parse(time.RFC3339, fmt.Sprint(e["expiresAt"]))
		if exp.After(time.Now()) {
			current = e
			continue
		}
		if err := zc.Delete(ctx, ziti.Enrollments, e.ID()); err != nil {
			return "", err
		}
		r.Recorder.Eventf(id, "Normal", "EnrollmentExpired", "deleted expired enrollment")
	}
	if current == nil {
		newID, err := zc.Create(ctx, ziti.Enrollments, ziti.Entity{
			"method":     "ott",
			"identityId": id.Status.ZitiID,
			"expiresAt":  time.Now().Add(enrollmentTTL).UTC().Format(time.RFC3339),
		})
		if err != nil {
			return "", err
		}
		r.Recorder.Eventf(id, "Normal", "EnrollmentCreated", "created enrollment")
		if enrollments, err = zc.List(ctx, ziti.Enrollments, fmt.Sprintf(`identity="%s"`, id.Status.ZitiID)); err != nil {
			return "", err
		}
		for _, e := range enrollments {
			if e.ID() == newID {
				current = e
			}
		}
		if current == nil {
			return "", fmt.Errorf("enrollment %s not found after create", newID)
		}
	}
	jwt, _ := current["jwt"].(string)
	if jwt == "" {
		return "", errors.New("enrollment has no jwt")
	}
	if exp, err := time.Parse(time.RFC3339, fmt.Sprint(current["expiresAt"])); err == nil {
		id.Status.EnrollmentExpiresAt = &metav1.Time{Time: exp}
	}

	return jwt, nil
}

func (r *ZitiIdentityReconciler) writeSecret(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, dataKey string, value []byte) error {
	var secret corev1.Secret
	key := r.secretKey(id)
	err := r.Get(ctx, key, &secret)
	switch {
	case apierrors.IsNotFound(err):
		secret = corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name}}
		if err := controllerutil.SetControllerReference(id, &secret, r.Scheme); err != nil {
			return err
		}
		secret.Data = map[string][]byte{dataKey: value}
		return r.Create(ctx, &secret)
	case err != nil:
		return err
	case !metav1.IsControlledBy(&secret, id):
		return &specError{"SecretConflict", fmt.Sprintf("Secret %s exists and is not owned by this ZitiIdentity", key.Name)}
	case string(secret.Data[dataKey]) == string(value):
		return nil
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[dataKey] = value
	return r.Update(ctx, &secret)
}

func (r *ZitiIdentityReconciler) dropJWT(ctx context.Context, id *zitiv1alpha1.ZitiIdentity) error {
	var secret corev1.Secret
	if err := r.Get(ctx, r.secretKey(id), &secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if _, ok := secret.Data[SecretKeyJWT]; !ok || !metav1.IsControlledBy(&secret, id) {
		return nil
	}
	delete(secret.Data, SecretKeyJWT)
	return r.Update(ctx, &secret)
}

func setIdentityCond(id *zitiv1alpha1.ZitiIdentity, typ string, ok bool, reasonTrue, reasonFalse, msg string) {
	c := metav1.Condition{Type: typ, ObservedGeneration: id.Generation}
	if ok {
		c.Status, c.Reason = metav1.ConditionTrue, reasonTrue
	} else {
		c.Status, c.Reason, c.Message = metav1.ConditionFalse, reasonFalse, msg
	}
	meta.SetStatusCondition(&id.Status.Conditions, c)
}

func markIdentityFailed(id *zitiv1alpha1.ZitiIdentity, reason, msg string) {
	for _, t := range []string{CondSynced, CondReady} {
		setIdentityCond(id, t, false, "", reason, msg)
	}
}

func (r *ZitiIdentityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiIdentity{}).
		Owns(&corev1.Secret{}).
		Named("zitiidentity").
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
