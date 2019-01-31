package main

import (
	"sync"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
)

type Buildkite interface {
	ListBuilds(from time.Time) ([]buildkite.Build, error)
}

type CachingBuildkite struct {
	upstream Buildkite
	duration time.Duration

	cache []buildkite.Build
	key   time.Time
	m     sync.Mutex
}

func NewCachingBuildkite(b Buildkite, d time.Duration) *CachingBuildkite {
	return &CachingBuildkite{
		upstream: b,
		duration: d,
	}
}

func (b *CachingBuildkite) ListBuilds(from time.Time) ([]buildkite.Build, error) {
	b.m.Lock()
	defer b.m.Unlock()

	cacheKey := from.Truncate(5 * time.Minute)
	if b.key == cacheKey {
		return b.cache, nil
	}

	builds, err := b.upstream.ListBuilds(from)
	if err == nil {
		b.cache = builds
		b.key = cacheKey
	}
	return builds, err

}

type NetworkBuildkite struct {
	Client *buildkite.Client
	Org    string
	Branch string
}

func (b *NetworkBuildkite) ListBuilds(from time.Time) ([]buildkite.Build, error) {
	opts := &buildkite.BuildsListOptions{
		ListOptions: buildkite.ListOptions{
			Page:    0,
			PerPage: 50,
		},
		CreatedFrom: from,
		State:       []string{"passed"},
	}
	if b.Branch != "" {
		opts.Branch = b.Branch
	}
	var result []buildkite.Build
	for {
		builds, resp, err := b.Client.Builds.ListByOrg(b.Org, opts)
		if err != nil {
			return result, err
		}
		result = append(result, builds...)

		if resp.NextPage <= 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	return result, nil
}
