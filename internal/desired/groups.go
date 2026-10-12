// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// Group is one Ziti service an app expands to: the exposed ports it captures and the targets that serve them.
type Group struct {
	Name    string
	Ports   []intstr.IntOrString
	Targets []zitiv1.Target
}

// Groups splits an app into one Ziti service per distinct target port set. A target that does not set ports
// serves every exposed port. When all targets serve the same port set there is one group, named like the app.
func (b *Builder) Groups() ([]Group, error) {
	all := b.Svc.Spec.Expose.Ports
	targets := b.Svc.Spec.Targets
	if len(targets) == 0 {
		return nil, fmt.Errorf("targets needs at least one entry")
	}

	type bucket struct {
		ports   []intstr.IntOrString
		targets []zitiv1.Target
	}
	var order []string
	byKey := map[string]*bucket{}
	for _, t := range targets {
		ports, key, err := targetPorts(t, all)
		if err != nil {
			return nil, err
		}
		if byKey[key] == nil {
			byKey[key] = &bucket{ports: ports}
			order = append(order, key)
		}
		byKey[key].targets = append(byKey[key].targets, t)
	}

	owner := map[string]string{}
	for _, key := range order {
		for _, p := range byKey[key].ports {
			token := portToken(p)
			if other, dup := owner[token]; dup && other != key {
				return nil, fmt.Errorf("exposed port %s is served by more than one target group", p.String())
			}
			owner[token] = key
		}
	}
	for _, p := range all {
		if _, ok := owner[portToken(p)]; !ok {
			return nil, fmt.Errorf("exposed port %s is not served by any target", p.String())
		}
	}

	groups := make([]Group, 0, len(order))
	for _, key := range order {
		groups = append(groups, Group{Ports: byKey[key].ports, Targets: byKey[key].targets})
	}
	if len(groups) == 1 {
		groups[0].Name = b.ZitiName()
		return groups, nil
	}
	for i := range groups {
		groups[i].Name = b.ZitiName() + "-" + groupSuffix(groups[i].Ports)
		if len(groups[i].Name)+len("-intercept.v1") > 1024 {
			return nil, fmt.Errorf("service name %q is too long, shorten zitiName", groups[i].Name)
		}
	}
	return groups, nil
}

func targetPorts(t zitiv1.Target, all []intstr.IntOrString) ([]intstr.IntOrString, string, error) {
	ports := t.ExposedPorts
	if len(ports) == 0 {
		ports = all
	}
	inExpose := map[string]bool{}
	for _, p := range all {
		inExpose[portToken(p)] = true
	}
	byToken := map[string]intstr.IntOrString{}
	var tokens []string
	for _, p := range ports {
		token := portToken(p)
		if !inExpose[token] {
			return nil, "", fmt.Errorf("target port %s is not in expose.ports", p.String())
		}
		if _, ok := byToken[token]; !ok {
			byToken[token] = p
			tokens = append(tokens, token)
		}
	}
	slices.Sort(tokens)
	out := make([]intstr.IntOrString, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, byToken[token])
	}
	return out, strings.Join(tokens, ","), nil
}

// portToken normalizes a port so grouping compares numbers and equal ranges as one value.
func portToken(p intstr.IntOrString) string {
	if p.Type == intstr.Int {
		return strconv.Itoa(int(p.IntVal))
	}
	return strings.ReplaceAll(strings.TrimSpace(p.StrVal), "-", "_")
}

func groupSuffix(ports []intstr.IntOrString) string {
	tokens := make([]string, 0, len(ports))
	for _, p := range ports {
		tokens = append(tokens, portToken(p))
	}
	slices.Sort(tokens)
	return strings.Join(tokens, "-")
}
