// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// upsertOwnedSecret sets one key of a Secret that the owner controls. It creates the Secret when it is missing.
// A Secret with the same name that the owner does not control gives SecretConflict.
// The manager cache holds only labeled Secrets, so an unlabeled Secret shows up as AlreadyExists on create.
func upsertOwnedSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, key types.NamespacedName, dataKey string, value []byte) error {
	return upsertOwnedSecretKeys(ctx, c, scheme, owner, key, map[string][]byte{dataKey: value})
}

// upsertOwnedSecretKeys sets several keys of a Secret that the owner controls, in one write.
func upsertOwnedSecretKeys(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, key types.NamespacedName, values map[string][]byte) error {
	var secret corev1.Secret
	err := c.Get(ctx, key, &secret)
	switch {
	case apierrors.IsNotFound(err):
		secret = corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name,
			Labels: map[string]string{ManagedByLabel: ManagedByLabelValue}}}
		if err := controllerutil.SetControllerReference(owner, &secret, scheme); err != nil {
			return err
		}
		secret.Data = values
		err = c.Create(ctx, &secret)
		if apierrors.IsAlreadyExists(err) {
			return secretConflict(key)
		}
		return err
	case err != nil:
		return err
	case !metav1.IsControlledBy(&secret, owner):
		return secretConflict(key)
	}
	changed := false
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	for k, v := range values {
		if string(secret.Data[k]) != string(v) {
			secret.Data[k] = v
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return c.Update(ctx, &secret)
}

// dropSecretKey removes keys from a Secret that the owner controls. A missing Secret or key is fine.
func dropSecretKey(ctx context.Context, c client.Client, owner client.Object, key types.NamespacedName, dataKeys ...string) error {
	var secret corev1.Secret
	if err := c.Get(ctx, key, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(&secret, owner) {
		return nil
	}
	changed := false
	for _, k := range dataKeys {
		if _, ok := secret.Data[k]; ok {
			delete(secret.Data, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return c.Update(ctx, &secret)
}

func secretConflict(key types.NamespacedName) error {
	return &specError{"SecretConflict", fmt.Sprintf("Secret %s/%s exists and is not owned by this resource", key.Namespace, key.Name)}
}
