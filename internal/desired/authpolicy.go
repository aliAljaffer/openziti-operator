// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"encoding/json"
	"fmt"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func boolOrFalse(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

func AuthPolicyEntity(o *zitiv1.ZitiAuthPolicy, conn *zitiv1.ZitiConnection, allowedSignerIDs map[string]string) (ziti.Entity, error) {
	secondary := map[string]any{"requireTotp": o.Spec.Secondary.RequireTotp}
	if o.Spec.Secondary.RequireExtJwtSigner != "" {
		id, ok := allowedSignerIDs[o.Spec.Secondary.RequireExtJwtSigner]
		if !ok {
			return nil, &MissingError{Msg: fmt.Sprintf("secondary.requireExtJwtSigner: no signer named %q in Ziti", o.Spec.Secondary.RequireExtJwtSigner)}
		}
		secondary["requireExtJwtSigner"] = id
	}
	signers := make([]string, 0, len(o.Spec.Primary.ExtJWT.AllowedSigners))
	for _, name := range o.Spec.Primary.ExtJWT.AllowedSigners {
		id, ok := allowedSignerIDs[name]
		if !ok {
			return nil, &MissingError{Msg: fmt.Sprintf("primary.extJwt.allowedSigners: no signer named %q in Ziti", name)}
		}
		signers = append(signers, id)
	}
	minLength := o.Spec.Primary.Updb.MinPasswordLength
	if minLength == 0 {
		minLength = 5
	}
	return ziti.Entity{
		"name": EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"primary": map[string]any{
			"cert": map[string]any{
				keyAllowed:          boolOrTrue(o.Spec.Primary.Cert.Allowed),
				"allowExpiredCerts": o.Spec.Primary.Cert.AllowExpiredCerts,
			},
			"extJwt": map[string]any{
				keyAllowed:       boolOrFalse(o.Spec.Primary.ExtJWT.Allowed),
				"allowedSigners": signers,
			},
			"updb": map[string]any{
				keyAllowed:               boolOrFalse(o.Spec.Primary.Updb.Allowed),
				"lockoutDurationMinutes": o.Spec.Primary.Updb.LockoutDurationMinutes,
				"maxAttempts":            o.Spec.Primary.Updb.MaxAttempts,
				"minPasswordLength":      minLength,
				"requireMixedCase":       o.Spec.Primary.Updb.RequireMixedCase,
				"requireNumberChar":      o.Spec.Primary.Updb.RequireNumberChar,
				"requireSpecialChar":     o.Spec.Primary.Updb.RequireSpecialChar,
			},
		},
		"secondary": secondary,
		"tags":      entityTags(conn, "ZitiAuthPolicy", &o.ObjectMeta),
	}, nil
}

func PostureCheckEntity(o *zitiv1.ZitiPostureCheck, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	attrs, err := ScopeAttributes(scopeFor(conn), o.Namespace, o.Spec.RoleAttributes)
	if err != nil {
		return nil, fmt.Errorf("roleAttributes: %w", err)
	}
	data := map[string]any{}
	if len(o.Spec.Data.Raw) > 0 {
		if err := json.Unmarshal(o.Spec.Data.Raw, &data); err != nil {
			return nil, fmt.Errorf("data must be an object: %w", err)
		}
	}
	if len(data) == 0 && o.Spec.Type != "MFA" {
		return nil, fmt.Errorf("data is required for posture check type %s", o.Spec.Type)
	}
	e := ziti.Entity{
		"name":           EntityName(&o.Spec.EntitySpec, &o.ObjectMeta),
		"typeId":         o.Spec.Type,
		"roleAttributes": attrs,
		"tags":           entityTags(conn, "ZitiPostureCheck", &o.ObjectMeta),
	}
	for k, v := range data {
		switch k {
		case "id", "name", "typeId", "roleAttributes", "tags", "createdAt", "updatedAt", "links":
			continue
		}
		e[k] = v
	}
	return e, nil
}
