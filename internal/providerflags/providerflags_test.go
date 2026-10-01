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

package providerflags

import (
	"flag"
	"strings"
	"testing"
)

func parse(t *testing.T, args ...string) *Flags {
	t.Helper()
	var f Flags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.Bind(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &f
}

func TestDecodeUpdbConnection(t *testing.T) {
	f := parse(t,
		"--connection.management-url=https://ctrl/edge/management/v1",
		"--connection.ca-bundle-configmap=ziti-root-ca",
		"--connection.auth.updb.secret=ziti-operator-credential",
		"--connection.hosting-routers=router1, router2",
	)
	spec, err := f.Decode("ziti-operator-system")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if spec.ManagementURL != "https://ctrl/edge/management/v1" {
		t.Errorf("managementUrl = %q", spec.ManagementURL)
	}
	if spec.CABundle.ConfigMapRef.Namespace != "ziti-operator-system" || spec.CABundle.ConfigMapRef.Name != "ziti-root-ca" {
		t.Errorf("caBundle = %+v", spec.CABundle.ConfigMapRef)
	}
	if got := spec.CABundle.ConfigMapRef.Key; got != "ca.crt" {
		t.Errorf("ca bundle key = %q, want ca.crt", got)
	}
	if spec.Auth.Updb == nil || spec.Auth.Updb.SecretRef.Name != "ziti-operator-credential" {
		t.Errorf("updb auth = %+v", spec.Auth.Updb)
	}
	if spec.Auth.Updb.SecretRef.Namespace != "ziti-operator-system" {
		t.Errorf("secret namespace = %q, want the operator namespace", spec.Auth.Updb.SecretRef.Namespace)
	}
	if spec.Auth.Cert != nil {
		t.Error("cert auth must not be set alongside updb")
	}
	if len(spec.HostingRouters) != 2 || spec.HostingRouters[0] != "router1" || spec.HostingRouters[1] != "router2" {
		t.Errorf("hostingRouters = %v", spec.HostingRouters)
	}
	if spec.RoleScope != "Namespaced" || spec.ClusterID != "default" {
		t.Errorf("defaults = %q / %q", spec.RoleScope, spec.ClusterID)
	}
}

func TestDecodeCertConnectionAndOverrides(t *testing.T) {
	f := parse(t,
		"--connection.management-url=https://ctrl/edge/management/v1",
		"--connection.ca-bundle-configmap=my-ca",
		"--connection.ca-bundle-key=custom.crt",
		"--connection.auth.cert.secret=ziti-client-cert",
		"--connection.secret-namespace=ziti",
		"--connection.cluster-id=prod",
		"--connection.role-scope=Global",
		"--connection.hosting-routers=router1",
		"--connection.entry-routers=router1",
		"--connection.default-edge-routers=router1",
	)
	spec, err := f.Decode("ziti-operator-system")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if spec.Auth.Cert == nil || spec.Auth.Cert.SecretRef.Namespace != "ziti" || spec.Auth.Cert.SecretRef.Name != "ziti-client-cert" {
		t.Errorf("cert auth = %+v", spec.Auth.Cert)
	}
	if spec.Auth.Updb != nil {
		t.Error("updb auth must not be set alongside cert")
	}
	if spec.CABundle.ConfigMapRef.Key != "custom.crt" {
		t.Errorf("ca bundle key = %q", spec.CABundle.ConfigMapRef.Key)
	}
	if spec.RoleScope != "Global" || spec.ClusterID != "prod" {
		t.Errorf("overrides = %q / %q", spec.RoleScope, spec.ClusterID)
	}
	if len(spec.EntryRouters) != 1 || len(spec.DefaultEdgeRouters) != 1 {
		t.Errorf("router lists = %v / %v", spec.EntryRouters, spec.DefaultEdgeRouters)
	}
}

func TestDecodeRejectsIncompleteFlags(t *testing.T) {
	cases := map[string]*Flags{
		"no url": parse(t,
			"--connection.ca-bundle-configmap=ca",
			"--connection.auth.updb.secret=s",
			"--connection.hosting-routers=r",
		),
		"no routers": parse(t,
			"--connection.management-url=https://ctrl/edge/management/v1",
			"--connection.ca-bundle-configmap=ca",
			"--connection.auth.updb.secret=s",
		),
		"no auth": parse(t,
			"--connection.management-url=https://ctrl/edge/management/v1",
			"--connection.ca-bundle-configmap=ca",
			"--connection.hosting-routers=r",
		),
		"both auth": parse(t,
			"--connection.management-url=https://ctrl/edge/management/v1",
			"--connection.ca-bundle-configmap=ca",
			"--connection.auth.updb.secret=s",
			"--connection.auth.cert.secret=s",
			"--connection.hosting-routers=r",
		),
		"no ca": parse(t,
			"--connection.management-url=https://ctrl/edge/management/v1",
			"--connection.auth.updb.secret=s",
			"--connection.hosting-routers=r",
		),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.Decode("ns"); err == nil {
				t.Error("Decode must fail")
			}
		})
	}
}

func TestDecodeRejectsEmptyNamespace(t *testing.T) {
	f := parse(t,
		"--connection.management-url=https://ctrl/edge/management/v1",
		"--connection.ca-bundle-configmap=ca",
		"--connection.auth.updb.secret=s",
		"--connection.hosting-routers=r",
	)
	if _, err := f.Decode(""); err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Errorf("Decode with an empty namespace = %v", err)
	}
}
