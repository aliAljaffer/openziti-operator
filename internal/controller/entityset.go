// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// entitySet creates, updates, and prunes the Ziti entities that one CR owns, matched by uid tag and name.
type entitySet struct {
	zc       ziti.Client
	rec      record.EventRecorder
	obj      client.Object
	kinds    []ziti.Kind
	existing map[ziti.Kind]map[string]ziti.Entity
	keep     map[ziti.Kind]map[string]bool
	adopt    bool
}

func newEntitySet(ctx context.Context, zc ziti.Client, rec record.EventRecorder, obj client.Object, uid types.UID, kinds []ziti.Kind) (*entitySet, error) {
	s := &entitySet{zc: zc, rec: rec, obj: obj, kinds: kinds,
		existing: map[ziti.Kind]map[string]ziti.Entity{}, keep: map[ziti.Kind]map[string]bool{}}
	for _, kind := range kinds {
		list, err := zc.List(ctx, kind, tagFilter(uid))
		if err != nil {
			return nil, err
		}
		s.existing[kind] = map[string]ziti.Entity{}
		s.keep[kind] = map[string]bool{}
		for _, e := range list {
			s.existing[kind][e.Name()] = e
		}
	}
	return s, nil
}

func (s *entitySet) ensure(ctx context.Context, kind ziti.Kind, body ziti.Entity) (string, error) {
	if ex, ok := s.existing[kind][body.Name()]; ok {
		s.keep[kind][ex.ID()] = true
		if !desired.Matches(body, ex) {
			if err := s.zc.Update(ctx, kind, ex.ID(), body); err != nil {
				return "", err
			}
			s.rec.Eventf(s.obj, "Normal", "Updated", "updated %s %s", kind, body.Name())
		}
		return ex.ID(), nil
	}
	clash, err := s.zc.List(ctx, kind, fmt.Sprintf(`name="%s"`, body.Name()))
	if err != nil {
		return "", err
	}
	if len(clash) > 0 && s.adopt {
		return s.adoptClash(ctx, kind, body, clash[0])
	}
	if len(clash) > 0 {
		return "", &specError{"NameConflict", fmt.Sprintf("%s %q already exists and is not managed by this operator", kind, body.Name())}
	}
	id, err := s.zc.Create(ctx, kind, body)
	if err != nil {
		return "", err
	}
	s.keep[kind][id] = true
	s.rec.Eventf(s.obj, "Normal", "Created", "created %s %s", kind, body.Name())
	return id, nil
}

// adoptClash takes over a hand-made entity: it merges the ownership tags into the desired body and replaces the entity.
func (s *entitySet) adoptClash(ctx context.Context, kind ziti.Kind, body, clash ziti.Entity) (string, error) {
	if owner, _ := clash.Tags()[desired.TagUID].(string); owner != "" {
		return "", &specError{"NameConflict", fmt.Sprintf("%s %q is already managed by another resource", kind, body.Name())}
	}
	tags := map[string]any{}
	for k, v := range clash.Tags() {
		tags[k] = v
	}
	if bt, ok := body["tags"].(map[string]any); ok {
		for k, v := range bt {
			tags[k] = v
		}
	}
	tags[desired.TagAdopted] = "true"
	body["tags"] = tags
	if err := s.zc.Update(ctx, kind, clash.ID(), body); err != nil {
		return "", err
	}
	s.keep[kind][clash.ID()] = true
	s.rec.Eventf(s.obj, "Normal", "Adopted", "adopted %s %s", kind, body.Name())
	return clash.ID(), nil
}

// prune deletes owned entities that ensure did not keep, in the order of kinds.
func (s *entitySet) prune(ctx context.Context) error {
	for _, kind := range s.kinds {
		for _, e := range s.existing[kind] {
			if s.keep[kind][e.ID()] {
				continue
			}
			if isAdopted(e) {
				if err := s.releaseOne(ctx, kind, e); err != nil {
					return err
				}
				continue
			}
			if err := s.zc.Delete(ctx, kind, e.ID()); err != nil {
				return err
			}
			s.rec.Eventf(s.obj, "Normal", "Deleted", "deleted %s %s", kind, e.Name())
		}
	}
	return nil
}

// release removes the operator tags from every owned entity and leaves the entities in Ziti.
func (s *entitySet) release(ctx context.Context) error {
	for _, kind := range s.kinds {
		for _, e := range s.existing[kind] {
			if err := s.releaseOne(ctx, kind, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Ziti replaces the whole tag map on a write, so other tags stay. Configs need a full update, the other kinds a patch.
func (s *entitySet) releaseOne(ctx context.Context, kind ziti.Kind, e ziti.Entity) error {
	var err error
	tags := desired.ReleaseTags(e.Tags())
	if kind == ziti.Configs {
		err = s.zc.Update(ctx, kind, e.ID(), ziti.Entity{"name": e.Name(), "configTypeId": e["configTypeId"], "data": e["data"], "tags": tags})
	} else {
		err = s.zc.Patch(ctx, kind, e.ID(), ziti.Entity{"tags": tags})
	}
	if err != nil {
		return err
	}
	s.rec.Eventf(s.obj, "Normal", "Released", "released %s %s, it stays in Ziti", kind, e.Name())
	return nil
}
