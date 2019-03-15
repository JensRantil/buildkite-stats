package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
	"google.golang.org/appengine/memcache"
)

type Build struct {
	ID         string
	Pipeline   Pipeline
	FinishedAt *time.Time
	StartedAt  *time.Time
}

type Pipeline struct {
	Name string
}

// Mapping to an internal struct will use a lot less memory.
func newBuildFromBuildkite(b buildkite.Build) Build {
	res := Build{
		Pipeline: Pipeline{
			Name: *b.Pipeline.Name,
		},
		ID: *b.ID,
	}
	if b.StartedAt != nil {
		res.StartedAt = &b.StartedAt.Time
	}
	if b.FinishedAt != nil {
		res.FinishedAt = &b.FinishedAt.Time
	}
	return res
}

type Buildkite interface {
	ListBuilds(from time.Time) ([]Build, error)
}

type NetworkBuildkite struct {
	Client *buildkite.Client
	Org    string
	Branch string
	Cache  Cache
}

type Cache interface {
	Put(k string, v []byte, ttl time.Duration) error
	Get(k string) ([]byte, error)
}

const itemsPerPage = 100

func (b *NetworkBuildkite) ListBuilds(from time.Time) ([]Build, error) {
	opts := &buildkite.BuildsListOptions{
		ListOptions: buildkite.ListOptions{
			Page:    1,
			PerPage: itemsPerPage,
		},
		CreatedFrom: from,
		State:       []string{"passed"},
	}
	if b.Branch != "" {
		opts.Branch = b.Branch
	}

	var result []Build
	for {
		builds, resp, err := b.query(b.Org, opts)
		if err != nil {
			return nil, err
		}

		// Populate the cache.
		for i, build := range builds {
			if i > 0 {
				// We are not mapping builds between pages because there might
				// be new build which is being pushed while we are iterating.
				// If that happens, we could see the last build on previous
				// page being the same build as the first build on the current
				// page. If so, we would mapping the same build to itself,
				// which in turn would lead to circular dependencies in the
				// cache.
				_ = b.populateCache(builds[i-1], build)
			}
		}

		result = append(result, builds...)

		if resp.NextPage <= 0 {
			break
		}

		// Try to read from cache if possible.
		if len(result) > 0 {
			cached, err := b.tryFromCache(from, result[len(result)-1])
			result = append(result, cached...)
			if err == nil {
				// we managed to fetch all items from cache
				return result, nil
			} else {
				// there is a race condition here if there was a new build that came in while we were paging.
				resp.NextPage = len(result)/itemsPerPage + 1
			}
		}

		opts.ListOptions.Page = resp.NextPage
	}

	// due to the race condition mentioned above this function is needed to make sure we don't have race conditions
	result = removeDuplicates(result)

	return result, nil
}

func (b *NetworkBuildkite) query(org string, opts *buildkite.BuildsListOptions) ([]Build, *buildkite.Response, error) {
	bbuilds, resp, err := b.Client.Builds.ListByOrg(org, opts)
	if err != nil {
		return nil, resp, err
	}

	var result []Build
	for _, b := range bbuilds {
		result = append(result, newBuildFromBuildkite(b))
	}

	return result, resp, err
}

func (b *NetworkBuildkite) populateCache(current, next Build) error {
	var ttl time.Duration
	if next.FinishedAt == nil {
		// we consider the build to be updated quite soon
		ttl = 10 * time.Minute
	} else {
		// we don't consider the build to update ever again
		ttl = 30 * 24 * time.Hour
	}

	s, err := json.Marshal(next)
	if err != nil {
		log.Panicln(err)
	}

	return b.Cache.Put(current.ID, s, ttl)
}

func (b *NetworkBuildkite) tryFromCache(until time.Time, from Build) ([]Build, error) {
	var res []Build
	for {
		s, err := b.Cache.Get(from.ID)
		if err != nil {
			if err == memcache.ErrCacheMiss {
				break
			}
			return res, err
		}

		var b Build
		err = json.Unmarshal(s, &b)
		if err != nil {
			log.Panicln(err)
		}
		res = append(res, b)

		from = b
	}
	return res, nil
}

func removeDuplicates(builds []Build) (res []Build) {
	seen := make(map[string]struct{})
	for _, b := range builds {
		if _, alreadySeen := seen[b.ID]; alreadySeen {
			continue
		}
		res = append(res, b)
		seen[b.ID] = struct{}{}
	}
	return
}
