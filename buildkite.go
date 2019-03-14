package main

import (
	"sync"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
)

type Build struct {
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

type InMemCachingBuildkite struct {
	upstream Buildkite
	duration time.Duration

	cache []Build
	key   time.Time
	m     sync.Mutex
}

func NewInMemCachingBuildkite(b Buildkite, d time.Duration) *InMemCachingBuildkite {
	return &InMemCachingBuildkite{
		upstream: b,
		duration: d,
	}
}

func (b *InMemCachingBuildkite) ListBuilds(from time.Time) ([]Build, error) {
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

func (b *NetworkBuildkite) ListBuilds(from time.Time) ([]Build, error) {
	opts := &buildkite.BuildsListOptions{
		ListOptions: buildkite.ListOptions{
			Page:    1,
			PerPage: 100,
		},
		CreatedFrom: from,
		State:       []string{"passed"},
	}
	if b.Branch != "" {
		opts.Branch = b.Branch
	}
	var result []Build
	for {
		bbuilds, resp, err := b.Client.Builds.ListByOrg(b.Org, opts)
		if err != nil {
			return nil, err
		}

		for _, b := range bbuilds {
			result = append(result, newBuildFromBuildkite(b))
		}

		if resp.NextPage <= 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	return result, nil
}
