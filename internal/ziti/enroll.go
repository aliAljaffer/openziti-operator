// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"encoding/json"
	"errors"

	"github.com/openziti/sdk-golang/ziti/enroll"
)

// EnrollOTT enrolls with a one-time-token JWT and returns the identity.json content.
// The controller address inside the JWT must be reachable from the caller.
func EnrollOTT(jwt string) ([]byte, error) {
	claims, tok, err := enroll.ParseToken(jwt)
	if err != nil {
		return nil, err
	}
	if claims.EnrollmentMethod != "ott" {
		return nil, errors.New("enrollment method is not ott")
	}
	flags := enroll.EnrollmentFlags{Token: claims, JwtToken: tok, JwtString: jwt}
	if err := flags.KeyAlg.Set("EC"); err != nil {
		return nil, err
	}
	cfg, err := enroll.Enroll(flags)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(cfg, "", "  ")
}
