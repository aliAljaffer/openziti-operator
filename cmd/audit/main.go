// SPDX-License-Identifier: Apache-2.0

// Command audit reports problems in a Ziti network. It only reads.
//
// It is the same as `manager audit`. Use --connection to read a ZitiConnection from the cluster:
//
//	audit --connection default
//	audit --url https://ctrl:443/edge/management/v1 --ca-file ca.pem
package main

import (
	"os"

	"github.com/aliAljaffer/openziti-operator/internal/audit"
)

func main() {
	os.Exit(audit.Main(os.Args[1:]))
}
