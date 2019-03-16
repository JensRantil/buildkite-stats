package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
)

type Build struct {
	ID         string
	Pipeline   Pipeline
	FinishedAt *time.Time
	StartedAt  *time.Time
	CreatedAt  time.Time
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
		ID:        *b.ID,
		CreatedAt: b.CreatedAt.Time,
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
	to := time.Now()

	startDay := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
	endDay := startDay.Add(24 * time.Hour)

	var res []Build
	for startDay.Before(to) {
		var cacheTTL time.Duration
		if time.Now().Sub(minTime(endDay, to)) > 12*time.Hour {
			// Cache aggresively for older builds. We don't expect them to be
			// modified.
			cacheTTL = 60 * 24 * time.Hour
		} else {
			cacheTTL = 10 * time.Minute
		}

		b, err := b.listBuildsBetween(maxTime(startDay, from), minTime(endDay, to), cacheTTL)
		if err != nil {
			return res, err
		}
		res = append(res, b...)

		startDay, endDay = startDay.Add(24*time.Hour), endDay.Add(24*time.Hour)
	}

	return res, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	} else {
		return b
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	} else {
		return b
	}
}

func (b *NetworkBuildkite) listBuildsBetween(from, to time.Time, cacheTTL time.Duration) ([]Build, error) {
	cacheKey := fmt.Sprintf("%d-%d", from.Unix(), to.Unix())
	cached, err := b.tryFromCache(cacheKey)
	if err == nil {
		return cached, err
	}

	opts := &buildkite.BuildsListOptions{
		ListOptions: buildkite.ListOptions{
			Page:    1,
			PerPage: itemsPerPage,
		},
		CreatedFrom: from,
		CreatedTo:   to,

		// This implies that all `Build`s will have FinishedAt set.
		State: []string{"passed"},
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

		result = append(result, builds...)

		if resp.NextPage <= 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	_ = b.populateCache(cacheKey, result, cacheTTL)

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

func (b *NetworkBuildkite) populateCache(key string, builds []Build, ttl time.Duration) error {
	s, err := json.Marshal(builds)
	if err != nil {
		log.Panicln(err)
	}

	// Compressing to make this a bit more future proof in case we have a _lot_
	// of builds per key one day - memcache keys usually can't be larger than 1
	// MB. We could of course switch to serialize to something like less
	// verbose like protobuf, but let's keep it simple for now.
	s = compress(s)

	return b.Cache.Put(key, s, ttl)
}

func (b *NetworkBuildkite) tryFromCache(key string) ([]Build, error) {
	var res []Build
	s, err := b.Cache.Get(key)
	if err != nil {
		return res, err
	}

	s = decompress(s)

	err = json.Unmarshal(s, &res)
	if err != nil {
		log.Panicln(err)
	}

	return res, nil
}

func compress(b []byte) []byte {
	input := bytes.NewBuffer(b)
	output := bytes.NewBuffer(nil)
	r := gzip.NewWriter(output)
	_, _ = io.Copy(r, input)
	_ = r.Close()
	return output.Bytes()
}

func decompress(b []byte) []byte {
	input := bytes.NewBuffer(b)
	output := bytes.NewBuffer(nil)
	var err error
	r, err := gzip.NewReader(input)
	if err != nil {
		log.Panicln("unable to create gzip reader:", err)
	}
	_, err = io.Copy(output, r)
	if err != nil {
		log.Panicln("unable to decompress:", err)
	}
	err = r.Close()
	if err != nil {
		log.Panicln("unable to Close when decompressing:", err)
	}
	return output.Bytes()
}
