// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"strings"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// IdentityMountPath is where the sidecar mounts the identity file of its ZitiIdentity.
const IdentityMountPath = "/ziti"

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
		b.WriteString("# Host mode rewrites iptables in the pod network namespace, so the pod needs NET_ADMIN and NET_RAW.\n")
		b.WriteString("# It usually needs hostNetwork too, and that is the workload's own setting to make.\n")
	}
	b.WriteString("spec:\n  template:\n    spec:\n")

	b.WriteString("      volumes:\n")
	b.WriteString("        - name: ziti-identity\n")
	b.WriteString("          secret:\n")
	fmt.Fprintf(&b, "            secretName: %q\n", identitySecretName(sc))
	b.WriteString("            optional: false\n")

	b.WriteString("      containers:\n")
	fmt.Fprintf(&b, "        - name: %s\n", sidecarContainerName)
	fmt.Fprintf(&b, "          image: %q\n", sidecarImage(version))
	b.WriteString("          imagePullPolicy: IfNotPresent\n")
	b.WriteString("          command:\n")
	for _, arg := range sidecarCommand(s) {
		fmt.Fprintf(&b, "            - %q\n", arg)
	}
	b.WriteString("          securityContext:\n")
	b.WriteString("            allowPrivilegeEscalation: false\n")
	b.WriteString("            readOnlyRootFilesystem: false\n")
	if s.Mode == "Host" {
		b.WriteString("            capabilities:\n")
		b.WriteString("              add:\n")
		b.WriteString("                - NET_ADMIN\n")
		b.WriteString("                - NET_RAW\n")
	}
	b.WriteString("          volumeMounts:\n")
	fmt.Fprintf(&b, "            - name: ziti-identity\n              mountPath: %s\n              readOnly: true\n", IdentityMountPath)

	return b.String()
}

const sidecarContainerName = "ziti-tunneler"

// sidecarCommand builds the tunneler command line. Proxy mode needs no resolver: it forwards to the addresses
// Ziti hands it rather than resolving names in the pod.
func sidecarCommand(s zitiv1.ZitiSidecarSpec) []string {
	cmd := []string{"ziti", "tunnel", strings.ToLower(s.Mode), "-i", IdentityMountPath + "/identity.json",
		"--svcPollRate", fmt.Sprint(s.ServicePollRate)}
	if s.Mode == "Host" {
		cmd = append(cmd, "-r", s.Resolver)
	}
	return cmd
}

func sidecarImage(version string) string {
	return "openziti/ziti-router:" + strings.TrimPrefix(version, "v")
}

// identitySecretName is the Secret the ZitiIdentity writes identity.json to. The identity uses its own name
// unless the resource names another Secret.
func identitySecretName(sc *zitiv1.ZitiSidecar) string { return sc.Spec.IdentityRef }
