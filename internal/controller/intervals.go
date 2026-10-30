// SPDX-License-Identifier: Apache-2.0

package controller

import "time"

// Intervals sets how soon the reconcilers look again. The manager flags override these values.
var (
	// ServiceResync is the healthy resync period for resources that match their Ziti entities.
	ServiceResync = 10 * time.Minute
	// DependencyRetry is how soon to look again when a resource names an entity that does not exist yet.
	// Such resources are often applied together, so waiting the long resync would feel broken.
	DependencyRetry = 30 * time.Second
	// PendingRecheck is how soon to look again while a certificate or enrollment is still pending.
	PendingRecheck = time.Minute
	// CAResync is short because the operator cannot watch the Secret of a cert-manager issuer.
	// A rotated CA is found within this time.
	CAResync = 2 * time.Minute
	// ConnectionResync is the healthy resync period for a ZitiConnection.
	ConnectionResync = 10 * time.Minute
	// ConnectionRetry is how soon to retry a ZitiConnection that cannot reach the controller.
	ConnectionRetry = time.Minute
)
