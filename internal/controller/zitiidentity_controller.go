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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	// ManagedByLabel marks the Secrets this operator creates. The manager cache holds only those.
	ManagedByLabel      = "app.kubernetes.io/managed-by"
	ManagedByLabelValue = "ziti-operator"
	defaultAuthPolicy   = "Default"
	enrollmentTTL       = 24 * time.Hour
	pendingRecheck      = time.Minute
	certWarnBefore      = 30 * 24 * time.Hour
	CondCertValid       = "CertificateValid"
)

type ZitiIdentityReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// Enroll turns an enrollment JWT into identity.json. Nil uses ziti.EnrollOTT.
	Enroll func(jwt string) ([]byte, error)
	// Extend renews the client certificate in identity.json. Nil uses ziti.ExtendCert.
	Extend func(identityJSON []byte, authenticatorID string) ([]byte, error)
	// SecretNamespaces are the namespaces where the operator may use Secrets. Empty means every namespace.
	SecretNamespaces []string
	// RenewBefore overrides the renewal window. Zero means the smaller of 30 days and a third of the certificate lifetime.
	RenewBefore time.Duration
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (r *ZitiIdentityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var id zitiv1alpha1.ZitiIdentity
	if err := r.Get(ctx, req.NamespacedName, &id); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !id.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &id)
	}
	if id.Spec.ManagementPolicy != zitiv1alpha1.ManagementObserve && controllerutil.AddFinalizer(&id, Finalizer) {
		if err := r.Update(ctx, &id); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := id.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, id.Spec.ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, id.Namespace)
	}
	if err == nil && len(r.SecretNamespaces) > 0 && !slices.Contains(r.SecretNamespaces, id.Namespace) && id.Spec.ManagementPolicy != zitiv1alpha1.ManagementObserve {
		err = &specError{"SecretNamespaceNotAllowed", fmt.Sprintf("the operator may not use Secrets in namespace %q, add it to rbac.secretNamespaces (--secret-namespaces)", id.Namespace)}
	}
	if err == nil {
		err = r.sync(ctx, &id, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil && id.Spec.Certificate != nil && !meta.IsStatusConditionTrue(id.Status.Conditions, CondReady):
		next.RequeueAfter = jitter(pendingRecheck)
	case err == nil && (id.Status.Enrolled || id.Spec.EnrollmentMode == zitiv1alpha1.EnrollmentNone):
		next.RequeueAfter = jitter(serviceResync)
	case err == nil:
		next.RequeueAfter = jitter(pendingRecheck)
	case errors.As(err, &se):
		markFailed(&id.Status.Conditions, id.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&id.Status.Conditions, id.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&id.Status.Conditions, id.Generation, "Error", err.Error())
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
	mode := id.Spec.ManagementPolicy
	orphan := id.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan
	if mode != zitiv1alpha1.ManagementObserve {
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
			if isAdopted(e) || orphan {
				if err := zc.Patch(ctx, ziti.Identities, e.ID(), ziti.Entity{"tags": desired.ReleaseTags(e.Tags())}); err != nil {
					return err
				}
				r.Recorder.Eventf(id, "Normal", "Released", "released identity %s, it stays in Ziti", e.Name())
				continue
			}
			if err := zc.Delete(ctx, ziti.Identities, e.ID()); err != nil {
				return err
			}
			r.Recorder.Eventf(id, "Normal", "Deleted", "deleted identity %s", e.Name())
		}
	}
	controllerutil.RemoveFinalizer(id, Finalizer)
	return r.Update(ctx, id)
}

// comparable drops the identity type. Ziti sets it once at creation and returns it as an object, not as a string.
func comparable(body ziti.Entity) ziti.Entity {
	c := maps.Clone(body)
	delete(c, "type")
	return c
}

func isAdopted(e ziti.Entity) bool { return e.Tags()[desired.TagAdopted] == "true" }

//nolint:gocyclo // one branch per enrollment mode and management policy, split when it grows
func (r *ZitiIdentityReconciler) sync(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.IdentityName(id, conn)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}

	mode := id.Spec.ManagementPolicy
	var body ziti.Entity
	if mode != zitiv1alpha1.ManagementObserve {
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
		if body, err = desired.Identity(id, conn, policies[0].ID()); err != nil {
			return &specError{"InvalidSpec", err.Error()}
		}
	}

	existing, err := zc.List(ctx, ziti.Identities, tagFilter(id.UID))
	if err != nil {
		return err
	}
	var zid ziti.Entity
	switch {
	case len(existing) > 0:
		zid = existing[0]
		if mode != zitiv1alpha1.ManagementObserve && !desired.Matches(comparable(body), zid) {
			if isAdopted(zid) {
				patch := ziti.Entity{"roleAttributes": body["roleAttributes"], "authPolicyId": body["authPolicyId"]}
				if ext, ok := body["externalId"]; ok {
					patch["externalId"] = ext
				}
				err = zc.Patch(ctx, ziti.Identities, zid.ID(), patch)
			} else {
				err = zc.Update(ctx, ziti.Identities, zid.ID(), body)
			}
			if err != nil {
				return err
			}
			r.Recorder.Eventf(id, "Normal", "Updated", "updated identity %s", name)
		}
	case mode == zitiv1alpha1.ManagementManage || mode == "":
		clash, err := zc.List(ctx, ziti.Identities, fmt.Sprintf(`name="%s"`, name))
		if err != nil {
			return err
		}
		if len(clash) > 0 {
			return &specError{"NameConflict", fmt.Sprintf("identity %q already exists and is not managed by this operator, use managementPolicy Adopt or Observe", name)}
		}
		create := ziti.Entity{}
		if id.Spec.EnrollmentMode != zitiv1alpha1.EnrollmentNone {
			create["enrollment"] = map[string]any{"ott": true}
		}
		maps.Copy(create, body)
		newID, err := zc.Create(ctx, ziti.Identities, create)
		if err != nil {
			return err
		}
		r.Recorder.Eventf(id, "Normal", "Created", "created identity %s", name)
		zid = ziti.Entity{"id": newID}
	default:
		found, err := zc.List(ctx, ziti.Identities, fmt.Sprintf(`name="%s"`, name))
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return &specError{"NotFound", fmt.Sprintf("identity %q not found in Ziti", name)}
		}
		zid = found[0]
		if mode == zitiv1alpha1.ManagementAdopt {
			if owner, _ := zid.Tags()[desired.TagUID].(string); owner != "" {
				return &specError{"NameConflict", fmt.Sprintf("identity %q is already managed by another ZitiIdentity", name)}
			}
			patch := ziti.Entity{
				"roleAttributes": body["roleAttributes"],
				"authPolicyId":   body["authPolicyId"],
				"tags":           desired.AdoptTags(conn, id, zid.Tags()),
			}
			if ext, ok := body["externalId"]; ok {
				patch["externalId"] = ext
			}
			err := zc.Patch(ctx, ziti.Identities, zid.ID(), patch)
			if err != nil {
				return err
			}
			r.Recorder.Eventf(id, "Normal", "Adopted", "adopted identity %s", name)
		}
	}

	id.Status.ZitiID, id.Status.ZitiName = zid.ID(), name
	setCond(&id.Status.Conditions, id.Generation, CondSynced, true, "Synced", "", "")

	// The identity list shows a non-empty authenticators map after enrollment.
	auth, _ := zid["authenticators"].(map[string]any)
	id.Status.Enrolled = len(auth) > 0
	operator := id.Spec.EnrollmentMode == zitiv1alpha1.EnrollmentOperator

	if id.Spec.EnrollmentMode == zitiv1alpha1.EnrollmentNone {
		// The identity logs in with a token. There is nothing to enroll, no Secret, and no certificate.
		id.Status.Enrolled, id.Status.EnrollmentExpiresAt, id.Status.CertNotAfter = false, nil, nil
		meta.RemoveStatusCondition(&id.Status.Conditions, CondCertValid)
		if id.Spec.Certificate != nil {
			ready, msg, err := r.ensureCertificate(ctx, id)
			if err != nil {
				return err
			}
			setCond(&id.Status.Conditions, id.Generation, CondReady, ready, "CertificateLogin", "CertificatePending", msg)
			return nil
		}
		setCond(&id.Status.Conditions, id.Generation, CondReady, true, "TokenLogin", "", "")
		return nil
	}
	if mode == zitiv1alpha1.ManagementObserve {
		return r.observeIdentity(ctx, id, zc)
	}

	switch {
	case id.Status.Enrolled && operator:
		var secret corev1.Secret
		err := r.Get(ctx, r.secretKey(id), &secret)
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		if len(secret.Data[SecretKeyIdentity]) > 0 {
			renewErr := r.renewIfDue(ctx, id, zc, &secret)
			if !errors.Is(renewErr, errCertRejected) {
				if renewErr != nil {
					return renewErr
				}
				id.Status.EnrollmentExpiresAt = nil
				setCond(&id.Status.Conditions, id.Generation, CondReady, true, "Enrolled", "", "")
				return r.trackCert(ctx, id, zc)
			}
		}
		if mode == zitiv1alpha1.ManagementAdopt {
			// The key lives with whoever enrolled the identity. Enrolling again would cut that holder off.
			setCond(&id.Status.Conditions, id.Generation, CondReady, false, "", "IdentityFileUnavailable",
				"the identity enrolled outside the operator, so there is no identity file to store. Re-enroll it in Ziti or use managementPolicy Manage")
			return nil
		}
		// The private key exists only in identity.json. Without a working copy the identity is unusable, so start over.
		if err := zc.Delete(ctx, ziti.Identities, zid.ID()); err != nil {
			return err
		}
		r.Recorder.Eventf(id, "Warning", "IdentityFileLost", "identity file is missing or its certificate is rejected, deleted identity to enroll again")
		id.Status.ZitiID, id.Status.Enrolled, id.Status.CertNotAfter = "", false, nil
		meta.RemoveStatusCondition(&id.Status.Conditions, CondCertValid)
		setCond(&id.Status.Conditions, id.Generation, CondReady, false, "", "IdentityFileLost", "identity file was lost, enrolling again")
		return nil
	case id.Status.Enrolled:
		id.Status.EnrollmentExpiresAt = nil
		setCond(&id.Status.Conditions, id.Generation, CondReady, true, "Enrolled", "", "")
		if err := r.dropJWT(ctx, id); err != nil {
			return err
		}
		return r.trackCert(ctx, id, zc)
	}

	setCond(&id.Status.Conditions, id.Generation, CondReady, false, "", "PendingEnrollment", "identity is not enrolled yet")
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
	setCond(&id.Status.Conditions, id.Generation, CondReady, true, "Enrolled", "", "")
	return r.trackCert(ctx, id, zc)
}

func (r *ZitiIdentityReconciler) observeIdentity(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, zc ziti.Client) error {
	if id.Status.Enrolled {
		id.Status.EnrollmentExpiresAt = nil
		setCond(&id.Status.Conditions, id.Generation, CondReady, true, "Enrolled", "", "")
		return r.trackCert(ctx, id, zc)
	}
	setCond(&id.Status.Conditions, id.Generation, CondReady, false, "", "PendingEnrollment", "identity is not enrolled yet")
	enrollments, err := zc.List(ctx, ziti.Enrollments, fmt.Sprintf(`identity="%s"`, id.Status.ZitiID))
	if err != nil {
		return err
	}
	id.Status.EnrollmentExpiresAt = nil
	for _, e := range enrollments {
		if exp, err := time.Parse(time.RFC3339, fmt.Sprint(e["expiresAt"])); err == nil {
			id.Status.EnrollmentExpiresAt = &metav1.Time{Time: exp}
		}
	}
	return nil
}

var errCertRejected = errors.New("controller rejected the identity certificate")

// renewIfDue extends the client certificate when little of its lifetime is left, then stores the new identity.json.
// Ziti rejects the old certificate once the new one is verified, so the Secret write must succeed.
// A failed renewal is only reported. trackCert keeps warning until the certificate expires.
func (r *ZitiIdentityReconciler) renewIfDue(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, zc ziti.Client, secret *corev1.Secret) error {
	file := secret.Data[SecretKeyIdentity]
	notBefore, notAfter, err := ziti.CertValidity(file)
	if err != nil {
		return nil
	}
	window := min(certWarnBefore, notAfter.Sub(notBefore)/3)
	if r.RenewBefore > 0 {
		window = r.RenewBefore
	}
	if time.Until(notAfter) > window {
		return nil
	}
	auths, err := zc.List(ctx, ziti.Authenticators, fmt.Sprintf(`identity="%s" and method="cert"`, id.Status.ZitiID))
	if err != nil {
		return err
	}
	if len(auths) == 0 {
		return nil
	}
	extend := r.Extend
	if extend == nil {
		extend = ziti.ExtendCert
	}
	next, err := extend(file, auths[0].ID())
	if err != nil {
		var ae *ziti.APIError
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return errCertRejected
		}
		r.Recorder.Eventf(id, "Warning", "RenewalFailed", "could not renew certificate: %v", err)
		return nil
	}
	for attempt := 0; ; attempt++ {
		if err = r.writeSecret(ctx, id, SecretKeyIdentity, next); err == nil {
			break
		}
		if attempt == 2 {
			return fmt.Errorf("store renewed identity file: %w", err)
		}
		time.Sleep(time.Second << attempt)
	}
	secret.Data[SecretKeyIdentity] = next
	r.Recorder.Eventf(id, "Normal", "CertificateRenewed", "renewed client certificate")
	return nil
}

// trackCert records the earliest client certificate expiry. It warns before expiry.
// An expired certificate sets Ready=False. The operator does not renew certificates.
func (r *ZitiIdentityReconciler) trackCert(ctx context.Context, id *zitiv1alpha1.ZitiIdentity, zc ziti.Client) error {
	auths, err := zc.List(ctx, ziti.Authenticators, fmt.Sprintf(`identity="%s"`, id.Status.ZitiID))
	if err != nil {
		return err
	}
	var earliest time.Time
	for _, a := range auths {
		pemText, _ := a["certPem"].(string)
		block, _ := pem.Decode([]byte(pemText))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if earliest.IsZero() || cert.NotAfter.Before(earliest) {
			earliest = cert.NotAfter
		}
	}
	if earliest.IsZero() {
		id.Status.CertNotAfter = nil
		meta.RemoveStatusCondition(&id.Status.Conditions, CondCertValid)
		return nil
	}
	id.Status.CertNotAfter = &metav1.Time{Time: earliest}

	left := time.Until(earliest)
	wasOK := !meta.IsStatusConditionFalse(id.Status.Conditions, CondCertValid)
	switch {
	case left <= 0:
		setCond(&id.Status.Conditions, id.Generation, CondCertValid, false, "", "Expired", "certificate expired "+earliest.UTC().Format(time.RFC3339))
		setCond(&id.Status.Conditions, id.Generation, CondReady, false, "", "CertExpired", "certificate expired "+earliest.UTC().Format(time.RFC3339))
	case left < certWarnBefore:
		msg := "certificate expires " + earliest.UTC().Format(time.RFC3339)
		setCond(&id.Status.Conditions, id.Generation, CondCertValid, false, "", "ExpiresSoon", msg)
	default:
		setCond(&id.Status.Conditions, id.Generation, CondCertValid, true, "Valid", "", "")
		return nil
	}
	if wasOK {
		r.Recorder.Eventf(id, "Warning", "CertificateExpiring", "certificate expires %s", earliest.UTC().Format(time.RFC3339))
	}
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
	return upsertOwnedSecret(ctx, r.Client, r.Scheme, id, r.secretKey(id), dataKey, value)
}

func (r *ZitiIdentityReconciler) dropJWT(ctx context.Context, id *zitiv1alpha1.ZitiIdentity) error {
	return dropSecretKey(ctx, r.Client, id, r.secretKey(id), SecretKeyJWT)
}

// ensureCertificate keeps the cert-manager Certificate of the identity and reports whether cert-manager issued it.
// The Secret with the key and certificate belongs to cert-manager, not to the operator.
func (r *ZitiIdentityReconciler) ensureCertificate(ctx context.Context, id *zitiv1alpha1.ZitiIdentity) (bool, string, error) {
	want := desired.WorkloadCertificate(id)
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(want.GroupVersionKind())
	err := r.Get(ctx, client.ObjectKeyFromObject(want), got)
	switch {
	case meta.IsNoMatchError(err):
		return false, "", &specError{"CertManagerMissing", "cert-manager is not installed: the Certificate kind does not exist"}
	case apierrors.IsNotFound(err):
		if err := controllerutil.SetControllerReference(id, want, r.Scheme); err != nil {
			return false, "", err
		}
		if err := r.Create(ctx, want); err != nil {
			return false, "", err
		}
		r.Recorder.Eventf(id, "Normal", "Created", "created Certificate %s", want.GetName())
		return false, "waiting for cert-manager to issue the certificate", nil
	case err != nil:
		return false, "", err
	case !metav1.IsControlledBy(got, id):
		return false, "", &specError{"CertificateConflict", fmt.Sprintf("Certificate %s exists and is not owned by this identity", want.GetName())}
	}
	wantSpec, _ := want.Object["spec"].(map[string]any)
	gotSpec, _ := got.Object["spec"].(map[string]any)
	if !desired.Matches(wantSpec, gotSpec) {
		got.Object["spec"] = want.Object["spec"]
		if err := r.Update(ctx, got); err != nil {
			return false, "", err
		}
		r.Recorder.Eventf(id, "Normal", "Updated", "updated Certificate %s", want.GetName())
		return false, "waiting for cert-manager to issue the certificate", nil
	}
	conds, _, _ := unstructured.NestedSlice(got.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Ready" {
			if m["status"] == "True" {
				return true, "", nil
			}
			msg, _ := m["message"].(string)
			return false, msg, nil
		}
	}
	return false, "waiting for cert-manager to issue the certificate", nil
}

func (r *ZitiIdentityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiIdentity{}).
		Owns(&corev1.Secret{}).
		Named("zitiidentity")
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiIdentityList{} },
		func(o *zitiv1alpha1.ZitiIdentity) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
