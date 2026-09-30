// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func SignerName(s *zitiv1.ZitiJwtSigner) string {
	if s.Spec.ZitiName != "" {
		return s.Spec.ZitiName
	}
	return s.Name
}

func boolOrTrue(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

// SignerWantsPolicy reports whether the signer gets an auth policy of the same name.
func SignerWantsPolicy(s *zitiv1.ZitiJwtSigner) bool { return boolOrTrue(s.Spec.CreateAuthPolicy) }

// JwtSigner builds the signer. With jwksEndpoint the controller fetches the keys. Otherwise certPEM and kid carry the key.
func JwtSigner(s *zitiv1.ZitiJwtSigner, conn *zitiv1.ZitiConnection, issuer, kid, certPEM string) ziti.Entity {
	claims := s.Spec.ClaimsProperty
	if claims == "" {
		claims = "sub"
	}
	e := ziti.Entity{
		"name":           SignerName(s),
		"enabled":        true,
		"issuer":         issuer,
		"audience":       s.Spec.Audience,
		"claimsProperty": claims,
		"useExternalId":  boolOrTrue(s.Spec.UseExternalID),
		"tags":           ownerTags(conn, "ZitiJwtSigner", &s.ObjectMeta),
	}
	if ep := s.Spec.Keys.JwksEndpoint; ep != nil {
		e["jwksEndpoint"] = ep.URL
	} else {
		e["certPem"] = certPEM
		e["kid"] = kid
	}
	return e
}

// SignerAuthPolicy builds an auth policy that accepts only tokens from the signer.
func SignerAuthPolicy(s *zitiv1.ZitiJwtSigner, conn *zitiv1.ZitiConnection, signerID string) ziti.Entity {
	return ziti.Entity{
		"name": SignerName(s),
		"primary": map[string]any{
			"cert":   map[string]any{"allowed": false, "allowExpiredCerts": false},
			"extJwt": map[string]any{"allowed": true, "allowedSigners": []string{signerID}},
			"updb": map[string]any{"allowed": false, "lockoutDurationMinutes": 0, "maxAttempts": 0, "minPasswordLength": 5,
				"requireMixedCase": false, "requireNumberChar": false, "requireSpecialChar": false},
		},
		"secondary": map[string]any{"requireTotp": false},
		"tags":      ownerTags(conn, "ZitiJwtSigner", &s.ObjectMeta),
	}
}
