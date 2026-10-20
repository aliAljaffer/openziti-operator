// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"strings"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// IdentityMountPath is where the sidecar mounts the identity file of its ZitiIdentity.
const IdentityMountPath = "/ziti"

// TunnelerRepository is the Host mode image: ziti-router with file capabilities on ziti and iptables, so tproxy
// runs as non-root. The tag is the controller version, built from images/tunneler/Dockerfile.
const TunnelerRepository = "ghcr.io/alialjaffer/ziti-tunneler"

// SidecarSecretKey is the Secret key the patch goes under.
const SidecarSecretKey = "sidecar-patch.yaml"

// SidecarPatch renders the strategic merge patch that adds the tunneler to a pod. The operator writes the patch
// and never edits the workload, because a workload it does not own is not its to change.
func SidecarPatch(sc *zitiv1.ZitiSidecar, version string) string {
	s := sc.Spec
	var b strings.Builder

	b.WriteString("# Add the Ziti tunneler to a workload with:\n")
	fmt.Fprintf(&b, "#   kubectl patch deployment %q -n %s --patch-file sidecar-patch.yaml --type=strategic\n", "<name>", sc.Namespace)
	if s.Mode == "Host" {
		b.WriteString("# Host mode runs tproxy in the pod network namespace as non-root with NET_ADMIN and NET_RAW.\n")
		b.WriteString("# The pod resolver points at the tunneler. Do not set hostNetwork: it changes the node iptables.\n")
	}
	b.WriteString("spec:\n  template:\n    spec:\n")
	if s.Mode == "Host" {
		b.WriteString("      securityContext:\n        sysctls:\n          - name: net.ipv4.ip_unprivileged_port_start\n            value: \"0\"\n")
		b.WriteString("      dnsPolicy: None\n")
		b.WriteString("      dnsConfig:\n        nameservers:\n          - 127.0.0.1\n")
	}

	b.WriteString("      volumes:\n")
	b.WriteString("        - name: ziti-identity\n")
	b.WriteString("          secret:\n")
	fmt.Fprintf(&b, "            secretName: %q\n", identitySecretName(sc))
	b.WriteString("            optional: false\n")

	b.WriteString("      containers:\n")
	fmt.Fprintf(&b, "        - name: %s\n", sidecarContainerName)
	fmt.Fprintf(&b, "          image: %q\n", sidecarImage(s, version))
	b.WriteString("          imagePullPolicy: IfNotPresent\n")
	b.WriteString("          command:\n")
	for _, arg := range sidecarCommand(s) {
		fmt.Fprintf(&b, "            - %q\n", arg)
	}
	b.WriteString("          securityContext:\n")
	b.WriteString("            allowPrivilegeEscalation: false\n")
	b.WriteString("            readOnlyRootFilesystem: false\n")
	if s.Mode == "Host" {
		b.WriteString("            runAsNonRoot: true\n")
		b.WriteString("            runAsUser: 2171\n")
		b.WriteString("            capabilities:\n")
		b.WriteString("              drop:\n                - ALL\n")
		b.WriteString("              add:\n")
		b.WriteString("                - NET_ADMIN\n")
		b.WriteString("                - NET_RAW\n")
	}
	b.WriteString("          volumeMounts:\n")
	fmt.Fprintf(&b, "            - name: ziti-identity\n              mountPath: %s\n              readOnly: true\n", IdentityMountPath)

	return b.String()
}

const sidecarContainerName = "ziti-tunneler"

// sidecarCommand builds the tunneler command line. Proxy mode needs no resolver or upstream: it forwards to the addresses
// Ziti hands it rather than resolving names in the pod.
func sidecarCommand(s zitiv1.ZitiSidecarSpec) []string {
	mode := "proxy"
	if s.Mode == "Host" {
		mode = "tproxy"
	}
	cmd := []string{"ziti", "tunnel", mode, "-i", IdentityMountPath + "/identity.json",
		"--svcPollRate", fmt.Sprint(s.ServicePollRate)}
	if s.Mode == "Host" {
		cmd = append(cmd, "-r", "udp://127.0.0.1:53")
		if s.Resolver != "" {
			cmd = append(cmd, "--dnsUpstream", s.Resolver)
		}
	}
	return cmd
}

func sidecarImage(s zitiv1.ZitiSidecarSpec, version string) string {
	switch {
	case s.Image != "":
		return s.Image
	case s.Mode == "Host":
		return TunnelerRepository + ":" + strings.TrimPrefix(version, "v")
	}
	return "openziti/ziti-router:" + strings.TrimPrefix(version, "v")
}

// identitySecretName is the Secret the ZitiIdentity writes identity.json to. The identity uses its own name
// unless the resource names another Secret.
func identitySecretName(sc *zitiv1.ZitiSidecar) string { return sc.Spec.IdentityRef }
