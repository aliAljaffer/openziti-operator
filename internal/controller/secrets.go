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
	var secret corev1.Secret
	err := c.Get(ctx, key, &secret)
	switch {
	case apierrors.IsNotFound(err):
		secret = corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name,
			Labels: map[string]string{ManagedByLabel: ManagedByLabelValue}}}
		if err := controllerutil.SetControllerReference(owner, &secret, scheme); err != nil {
			return err
		}
		secret.Data = map[string][]byte{dataKey: value}
		err = c.Create(ctx, &secret)
		if apierrors.IsAlreadyExists(err) {
			return secretConflict(key)
		}
		return err
	case err != nil:
		return err
	case !metav1.IsControlledBy(&secret, owner):
		return secretConflict(key)
	case string(secret.Data[dataKey]) == string(value):
		return nil
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[dataKey] = value
	return c.Update(ctx, &secret)
}

// dropSecretKey removes one key from a Secret that the owner controls. A missing Secret or key is fine.
func dropSecretKey(ctx context.Context, c client.Client, owner client.Object, key types.NamespacedName, dataKey string) error {
	var secret corev1.Secret
	if err := c.Get(ctx, key, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}
	if _, ok := secret.Data[dataKey]; !ok || !metav1.IsControlledBy(&secret, owner) {
		return nil
	}
	delete(secret.Data, dataKey)
	return c.Update(ctx, &secret)
}

func secretConflict(key types.NamespacedName) error {
	return &specError{"SecretConflict", fmt.Sprintf("Secret %s/%s exists and is not owned by this resource", key.Namespace, key.Name)}
}
