package main

import (
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
)

type Buildkite struct {
	Client *buildkite.Client
	Org    string
	Branch string
}

func (b *Buildkite) ListBuilds(from time.Time) ([]buildkite.Build, error) {
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
