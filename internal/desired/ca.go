// SPDX-License-Identifier: Apache-2.0

package desired

import (
	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func CAName(c *zitiv1.ZitiCA) string {
	if c.Spec.ZitiName != "" {
		return c.Spec.ZitiName
	}
	return c.Name
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// CA builds the certificate authority. Ziti requires identityNameFormat on every write, so it is always sent.
func CA(c *zitiv1.ZitiCA, conn *zitiv1.ZitiConnection, certPEM string) ziti.Entity {
	claim := c.Spec.ExternalIDClaim
	roles, format := []string{}, "[caName]-[commonName]"
	auto := c.Spec.AutoEnrollment
	if auto != nil {
		roles = append(roles, auto.IdentityRoles...)
		format = orDefault(auto.IdentityNameFormat, format)
	}
	return ziti.Entity{
		"name":                      CAName(c),
		"certPem":                   certPEM,
		"isAuthEnabled":             boolOrTrue(c.Spec.AuthEnabled),
		"isAutoCaEnrollmentEnabled": auto != nil,
		"isOttCaEnrollmentEnabled":  false,
		"identityRoles":             roles,
		"identityNameFormat":        format,
		"externalIdClaim": map[string]any{
			"location":        orDefault(claim.Location, "COMMON_NAME"),
			"matcher":         orDefault(claim.Matcher, "ALL"),
			"matcherCriteria": claim.MatcherCriteria,
			"parser":          orDefault(claim.Parser, "NONE"),
			"parserCriteria":  claim.ParserCriteria,
			"index":           claim.Index,
		},
		"tags": ownerTags(conn, "ZitiCA", &c.ObjectMeta),
	}
}
