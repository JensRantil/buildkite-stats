package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/go-chi/chi"
	chart "github.com/wcharczuk/go-chart"
)

type Routes struct {
	Buildkite Buildkite
}

func (wr *Routes) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", wr.root)
	r.Get("/charts/{pipeline}", wr.charts)

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

	wr.printCharts(w, r)

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

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	sums := make(map[string]time.Duration)
	for _, b := range builds {
		name := *b.Pipeline.Name
		sums[name] += b.FinishedAt.Time.Sub(b.StartedAt.Time)
	}

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

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	sums := make(map[string]time.Duration)
	counts := make(map[string]int)
	for _, b := range builds {
		name := *b.Pipeline.Name
		sums[name] += b.FinishedAt.Time.Sub(b.StartedAt.Time)
		counts[name] += 1
	}

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

func (wr *Routes) printCharts(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<h2>Average time spent building staging past 1 week</h2>`)

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	activePipelines := make(map[string]struct{})
	for _, b := range builds {
		name := *b.Pipeline.Name
		activePipelines[name] = struct{}{}
	}

	orderedList := make([]string, 0)
	for k, _ := range activePipelines {
		orderedList = append(orderedList, k)
	}
	sort.Strings(orderedList)

	for _, pipeline := range orderedList {
		fmt.Fprintf(w, `<h2>%s</h2><img src="/charts/%s" />`, pipeline, url.PathEscape(pipeline))
	}
}

type timelineDuration struct {
	When     time.Time
	Duration time.Duration
}
type timelineSlice []timelineDuration

func (d timelineSlice) Len() int           { return len(d) }
func (d timelineSlice) Less(i, j int) bool { return d[i].When.Before(d[j].When) }
func (d timelineSlice) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

func DurationValueFormatter(v interface{}) string {
	return time.Duration(time.Duration(v.(float64)) * time.Second).Truncate(time.Second).String()
}

func (wr *Routes) charts(w http.ResponseWriter, r *http.Request) {
	pipeline := chi.URLParam(r, "pipeline")

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	items := make(timelineSlice, 0)
	for _, b := range builds {
		name := *b.Pipeline.Name
		if name != pipeline {
			continue
		}
		items = append(items, timelineDuration{b.StartedAt.Time, b.FinishedAt.Time.Sub(b.StartedAt.Time)})
	}
	sort.Sort(items)

	ts := chart.TimeSeries{
		Style: chart.Style{
			DotWidth: 5,
			Show:     true,
		},
	}
	for _, sample := range items {
		ts.XValues = append(ts.XValues, sample.When)
		ts.YValues = append(ts.YValues, sample.Duration.Seconds())
	}

	graph := chart.Chart{
		XAxis: chart.XAxis{
			Style: chart.StyleShow(),
		},
		Series: []chart.Series{ts},
		Height: 350,
		Width:  980,
		YAxis: chart.YAxis{
			Name:           "Seconds",
			NameStyle:      chart.StyleShow(),
			Style:          chart.StyleShow(),
			ValueFormatter: DurationValueFormatter,
		},
	}

	w.Header().Set("Content-Type", "image/png")
	if err := graph.Render(chart.PNG, w); err != nil {
		log.Println(err)
	}
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
