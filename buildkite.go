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

func (b *Buildkite) ListBuilds(from time.Time, f func(buildkite.Build) error) error {
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
	for {
		builds, resp, err := b.Client.Builds.ListByOrg(b.Org, opts)
		if err != nil {
			return err
		}

		for _, build := range builds {
			if err := f(build); err != nil {
				return err
			}
		}

		if resp.NextPage <= 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	return nil
}
