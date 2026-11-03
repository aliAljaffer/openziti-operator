// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"context"
	"fmt"
	"sync"
)

// ListRequest is one List call for ListEach.
type ListRequest struct {
	Kind   Kind
	Filter string
}

// ListAll lists every kind with the same filter concurrently and returns the results in the same order as kinds.
func ListAll(ctx context.Context, c Client, kinds []Kind, filter string) ([][]Entity, error) {
	reqs := make([]ListRequest, len(kinds))
	for i, kind := range kinds {
		reqs[i] = ListRequest{Kind: kind, Filter: filter}
	}
	return ListEach(ctx, c, reqs)
}

// ListEach runs every request concurrently and returns the results in the same order.
// It waits for every call to finish and reports the error of the first request that failed.
func ListEach(ctx context.Context, c Client, reqs []ListRequest) ([][]Entity, error) {
	lists := make([][]Entity, len(reqs))
	errs := make([]error, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() {
			lists[i], errs[i] = c.List(ctx, req.Kind, req.Filter)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", reqs[i].Kind, err)
		}
	}
	return lists, nil
}
