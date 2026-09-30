// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"regexp"
	"strings"
)

// AddressPlaceholder stands in for an advertised address the user has not set.
const AddressPlaceholder = "CHANGE_ME.invalid"

// routerUID is the user of the ziti-router image. A fresh volume belongs to root, so the manifests hand it over.
const routerUID = 2171

// RouterManifest holds what a router needs to start on a VM or in a cluster.
type RouterManifest struct {
	Name, JWT, Address, Version string
	Port                        int32
}

var nonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

func (m RouterManifest) k8sName() string {
	n := strings.Trim(nonDNS.ReplaceAllString(strings.ToLower(m.Name), "-"), "-")
	if n == "" {
		n = "ziti-router"
	}
	return n
}

func (m RouterManifest) address() string {
	if m.Address == "" {
		return AddressPlaceholder
	}
	return m.Address
}

func (m RouterManifest) image() string {
	return "openziti/ziti-router:" + strings.TrimPrefix(m.Version, "v")
}

// env lists the variables that bring the ziti-router image to an enrolled, online router. The token is first.
func (m RouterManifest) env() [][2]string {
	return [][2]string{
		{"ZITI_ENROLL_TOKEN", m.JWT},
		{"ZITI_BOOTSTRAP", "true"},
		{"ZITI_BOOTSTRAP_CONFIG", "true"},
		{"ZITI_BOOTSTRAP_ENROLLMENT", "true"},
		{"ZITI_AUTO_RENEW_CERTS", "true"},
		{"ZITI_ROUTER_ADVERTISED_ADDRESS", m.address()},
		{"ZITI_ROUTER_PORT", fmt.Sprint(m.Port)},
	}
}

func (m RouterManifest) note() string {
	if m.Address != "" {
		return ""
	}
	return "# Replace CHANGE_ME.invalid with the address clients and other routers use to reach this router.\n"
}

// Compose returns a Docker Compose file that runs the router on a VM.
func (m RouterManifest) Compose() string {
	var env strings.Builder
	for _, kv := range m.env() {
		fmt.Fprintf(&env, "      %s: %q\n", kv[0], kv[1])
	}
	return fmt.Sprintf(`%sservices:
  volume-owner:
    image: busybox
    command: chown -R %d /ziti-router
    volumes:
      - router-data:/ziti-router
  router:
    image: %s
    restart: unless-stopped
    depends_on:
      volume-owner:
        condition: service_completed_successfully
    ports:
      - "%d:%d"
    environment:
%s    volumes:
      - router-data:/ziti-router
volumes:
  router-data:
`, m.note(), routerUID, m.image(), m.Port, m.Port, env.String())
}

// Deployment returns a Secret, Deployment, PersistentVolumeClaim, and Service that run the router in a cluster.
func (m RouterManifest) Deployment() string {
	n := m.k8sName()
	var env strings.Builder
	for _, kv := range m.env()[1:] {
		fmt.Fprintf(&env, "            - name: %s\n              value: %q\n", kv[0], kv[1])
	}
	return fmt.Sprintf(`%sapiVersion: v1
kind: Secret
metadata:
  name: %[2]s-enrollment
stringData:
  ZITI_ENROLL_TOKEN: %[3]q
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %[2]s-data
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 100Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[2]s
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: %[2]s
  template:
    metadata:
      labels:
        app: %[2]s
    spec:
      securityContext:
        fsGroup: %[4]d
      containers:
        - name: router
          image: %[5]s
          ports:
            - containerPort: %[6]d
          envFrom:
            - secretRef:
                name: %[2]s-enrollment
          env:
%[7]s          volumeMounts:
            - name: data
              mountPath: /ziti-router
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: %[2]s-data
---
apiVersion: v1
kind: Service
metadata:
  name: %[2]s
spec:
  type: LoadBalancer
  selector:
    app: %[2]s
  ports:
    - port: %[6]d
      targetPort: %[6]d
`, m.note(), n, m.JWT, routerUID, m.image(), m.Port, env.String())
}
