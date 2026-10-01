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

// Package providerflags holds the flags that define a ZitiConnection and turns
// them into a ZitiConnectionSpec. This keeps the flag surface and the CRD
// surface together, so the resource the operator creates always agrees with the
// manager's own configuration.
package providerflags

import (
	"errors"
	"flag"
	"strings"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

const (
	defaultCABundleKey = "ca.crt"
	defaultRoleScope   = "Namespaced"
	defaultClusterID   = "default"
)

// Flags is the connection flag set. Bind registers it; Decode loads a spec.
type Flags struct {
	ManagementURL      string
	CABundleConfigMap  string
	CABundleKey        string
	AuthUpdbSecret     string
	AuthCertSecret     string
	SecretNamespace    string
	RoleScope          string
	ClusterID          string
	HostingRouters     string
	EntryRouters       string
	DefaultEdgeRouters string
}

// Bind registers the flags on fs.
func (f *Flags) Bind(fs *flag.FlagSet) {
	fs.StringVar(&f.ManagementURL, "connection.management-url", "",
		"Edge Management API URL for a ZitiConnection the manager creates and uses.")
	fs.StringVar(&f.CABundleConfigMap, "connection.ca-bundle-configmap", "",
		"ConfigMap that holds the controller CA bundle for the created ZitiConnection.")
	fs.StringVar(&f.CABundleKey, "connection.ca-bundle-key", defaultCABundleKey,
		"Key in the CA bundle ConfigMap.")
	fs.StringVar(&f.AuthUpdbSecret, "connection.auth.updb.secret", "",
		"Secret with the keys username and password for the created ZitiConnection.")
	fs.StringVar(&f.AuthCertSecret, "connection.auth.cert.secret", "",
		"Secret with the keys tls.crt and tls.key for the created ZitiConnection.")
	fs.StringVar(&f.SecretNamespace, "connection.secret-namespace", "",
		"Namespace of the credential Secret. Defaults to the operator namespace.")
	fs.StringVar(&f.RoleScope, "connection.role-scope", defaultRoleScope,
		"Role scope of the created ZitiConnection: Namespaced or Global.")
	fs.StringVar(&f.ClusterID, "connection.cluster-id", defaultClusterID,
		"Cluster ID that separates operators sharing one Ziti network.")
	fs.StringVar(&f.HostingRouters, "connection.hosting-routers", "",
		"Comma separated routers a ZitiApp may run behind. Required when the manager creates the connection.")
	fs.StringVar(&f.EntryRouters, "connection.entry-routers", "",
		"Comma separated routers a ZitiApp entryRouters or access policy edgeRouters may name.")
	fs.StringVar(&f.DefaultEdgeRouters, "connection.default-edge-routers", "",
		"Comma separated routers added to every app's edge router policy.")
}

// Decode loads the spec for the created ZitiConnection. namespace is the
// operator namespace, where the CA bundle ConfigMap lives. It is an error to
// call Decode before the required flags are set.
func (f *Flags) Decode(namespace string) (zitiv1alpha1.ZitiConnectionSpec, error) {
	if f.ManagementURL == "" {
		return zitiv1alpha1.ZitiConnectionSpec{}, errors.New("--connection.management-url is required when the manager creates the connection")
	}
	hostingRouters := splitList(f.HostingRouters)
	if len(hostingRouters) == 0 {
		return zitiv1alpha1.ZitiConnectionSpec{}, errors.New("--connection.hosting-routers needs at least one router when the manager creates the connection")
	}
	if (f.AuthUpdbSecret == "") == (f.AuthCertSecret == "") {
		return zitiv1alpha1.ZitiConnectionSpec{}, errors.New("set exactly one of --connection.auth.updb.secret or --connection.auth.cert.secret when the manager creates the connection")
	}
	if namespace == "" {
		return zitiv1alpha1.ZitiConnectionSpec{}, errors.New("the manager creates a connection with a namespaced credential Secret; the operator namespace is unknown")
	}

	authNamespace := firstNonEmpty(f.SecretNamespace, namespace)
	caName := f.CABundleConfigMap
	if caName == "" {
		return zitiv1alpha1.ZitiConnectionSpec{}, errors.New("--connection.ca-bundle-configmap is required when the manager creates the connection")
	}

	spec := zitiv1alpha1.ZitiConnectionSpec{
		ManagementURL: f.ManagementURL,
		CABundle: zitiv1alpha1.CABundle{ConfigMapRef: zitiv1alpha1.ConfigMapKeyRef{
			Namespace: namespace,
			Name:      caName,
			Key:       firstNonEmpty(f.CABundleKey, defaultCABundleKey),
		}},
		RoleScope:          zitiv1alpha1.RoleScope(firstNonEmpty(f.RoleScope, defaultRoleScope)),
		ClusterID:          firstNonEmpty(f.ClusterID, defaultClusterID),
		HostingRouters:     hostingRouters,
		EntryRouters:       splitList(f.EntryRouters),
		DefaultEdgeRouters: splitList(f.DefaultEdgeRouters),
	}
	if f.AuthUpdbSecret != "" {
		spec.Auth.Updb = &zitiv1alpha1.UpdbAuth{SecretRef: zitiv1alpha1.SecretRef{Namespace: authNamespace, Name: f.AuthUpdbSecret}}
	} else {
		spec.Auth.Cert = &zitiv1alpha1.CertAuth{SecretRef: zitiv1alpha1.SecretRef{Namespace: authNamespace, Name: f.AuthCertSecret}}
	}
	return spec, nil
}

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for s := range strings.SplitSeq(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ErrNone is returned by Decode when no connection flags were set at all, so
// the caller can leave the connection to the user.
var ErrNone = errors.New("no connection flags set")

// Configured reports whether the minimum flags for a spec are present.
func (f *Flags) Configured() bool { return f.ManagementURL != "" }
