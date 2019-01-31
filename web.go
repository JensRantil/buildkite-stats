package main

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
	"github.com/go-chi/chi"
)

type Routes struct {
	Buildkite *Buildkite
}

func (wr *Routes) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", wr.root)

	return r
}

func (wr *Routes) root(w http.ResponseWriter, r *http.Request) {
	// TODO: https://github.com/UnnoTed/fileb0x for templates. See also
	// https://github.com/go-task/examples/blob/master/go-web-app/Taskfile.yml#L63
	fmt.Fprintf(w, `
		<html>
		<head><title>Buildkite dashboard</title></head>
		<body>
		<h1>Buildkite Dashboard</h1>`)

	wr.totalTopList(w, r)
	wr.averageTopList(w, r)

	fmt.Fprintf(w, `
		</body>
		</html>
		`)
}

type namedDuration struct {
	Name     string
	Duration time.Duration
}
type durationSlice []namedDuration

func (d durationSlice) Len() int           { return len(d) }
func (d durationSlice) Less(i, j int) bool { return d[i].Duration < d[j].Duration }
func (d durationSlice) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

func (wr *Routes) totalTopList(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<h2>Total time spent building staging past 1 week</h2>`)

	sums := make(map[string]time.Duration)
	wr.Buildkite.ListBuilds(fromTime(r), func(b buildkite.Build) error {
		name := *b.Pipeline.Name

		var t time.Duration
		for _, job := range b.Jobs {
			if job.State == nil || *job.State != "passed" {
				// A build also contains all jobs that aren't relevant for the
				// branch. They don't contain a FinishedAt and StartedAt field.
				continue
			}
			t += job.FinishedAt.Time.Sub(job.StartedAt.Time)
		}
		sums[name] += t
		return nil
	})

	sumsList := make(durationSlice, 0, len(sums))
	for k, v := range sums {
		sumsList = append(sumsList, namedDuration{k, v})
	}
	sort.Sort(sort.Reverse(sumsList))

	fmt.Fprintf(w, `<table><tr><th>Pipeline</th><th>Total Duration</th></tr>`)
	for _, pipeline := range sumsList {
		fmt.Fprintf(w, `<tr><th>%s</th><td>%s</td></tr>`, pipeline.Name, pipeline.Duration)
	}
	fmt.Fprintf(w, `</table>`)
}

func (wr *Routes) averageTopList(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<h2>Average time spent building staging past 1 week</h2>`)

	sums := make(map[string]time.Duration)
	counts := make(map[string]int)
	wr.Buildkite.ListBuilds(fromTime(r), func(b buildkite.Build) error {
		name := *b.Pipeline.Name

		var t time.Duration
		for _, job := range b.Jobs {
			if job.State == nil || *job.State != "passed" {
				// A build also contains all jobs that aren't relevant for the
				// branch. They don't contain a FinishedAt and StartedAt field.
				continue
			}
			t += job.FinishedAt.Time.Sub(job.StartedAt.Time)
		}
		sums[name] += t
		counts[name] += 1
		return nil
	})

	sumsList := make(durationSlice, 0, len(sums))
	for k, v := range sums {
		sumsList = append(sumsList, namedDuration{k, v / time.Duration(counts[k])})
	}
	sort.Sort(sort.Reverse(sumsList))

	fmt.Fprintf(w, `<table><tr><th>Pipeline</th><th>Average Duration</th></tr>`)
	for _, pipeline := range sumsList {
		fmt.Fprintf(w, `<tr><th>%s</th><td>%s</td></tr>`, pipeline.Name, pipeline.Duration.Truncate(time.Second))
	}
	fmt.Fprintf(w, `</table>`)
}

func nilToString(s *string) string {
	if s == nil {
		return "nil"
	}
	return *s
}

func fromTime(w *http.Request) time.Time {
	return time.Now().Add(-24 * 7 * time.Hour)
}
