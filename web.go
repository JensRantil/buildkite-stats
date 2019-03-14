package main

import (
	"container/ring"
	"fmt"
	"log"
	"math"
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
	r.Get("/rolling-average", wr.root)

	r.Get("/charts/{pipeline}/{mode}", wr.charts)
	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

	return r
}

func (wr *Routes) root(w http.ResponseWriter, r *http.Request) {

	chartMode := "all"
	if r.RequestURI == "/rolling-average" {
		chartMode = "rolling-average"
	}

	// TODO: https://github.com/UnnoTed/fileb0x for templates. See also
	// https://github.com/go-task/examples/blob/master/go-web-app/Taskfile.yml#L63
	fmt.Fprintf(w, `
		<html>
		<head><title>Buildkite dashboard</title></head>
		<body>
		<h1>Buildkite Dashboard</h1>`)

	wr.totalTopList(w, r)
	wr.percentileTopList(w, r, 90)

	wr.printCharts(w, r, chartMode)

	fmt.Fprintf(w, `
		</body>
		</html>
		`)
}

type namedDuration struct {
	Name     string
	Duration time.Duration
}
type namedDurationSlice []namedDuration

func (d namedDurationSlice) Len() int           { return len(d) }
func (d namedDurationSlice) Less(i, j int) bool { return d[i].Duration < d[j].Duration }
func (d namedDurationSlice) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

func (wr *Routes) totalTopList(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<h2>Total time spent building staging past 4 weeks</h2>`)

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

	sumsList := make(namedDurationSlice, 0, len(sums))
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

func (wr *Routes) percentileTopList(w http.ResponseWriter, r *http.Request, perc int) {
	fmt.Fprintf(w, `<h2>%dth percentile of time spent building staging past 4 weeks</h2>`, perc)
	fperc := float64(perc) / 100

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	durationsByPipeline := make(map[string][]time.Duration)
	for _, b := range builds {
		name := *b.Pipeline.Name
		durationsByPipeline[name] = append(durationsByPipeline[name], b.FinishedAt.Time.Sub(b.StartedAt.Time))
	}

	sumsList := make(namedDurationSlice, 0, len(durationsByPipeline))
	for k, v := range durationsByPipeline {
		sumsList = append(sumsList, namedDuration{k, durationPercentile(v, fperc)})
	}
	sort.Sort(sort.Reverse(sumsList))

	fmt.Fprintf(w, `<table><tr><th>Pipeline</th><th>%dth percentile</th></tr>`, perc)
	for _, pipeline := range sumsList {
		fmt.Fprintf(w, `<tr><th>%s</th><td>%s</td></tr>`, pipeline.Name, pipeline.Duration.Truncate(time.Second))
	}
	fmt.Fprintf(w, `</table>`)
}

type durationSlice []time.Duration

func (d durationSlice) Len() int           { return len(d) }
func (d durationSlice) Less(i, j int) bool { return d[i] < d[j] }
func (d durationSlice) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

func durationPercentile(a []time.Duration, perc float64) time.Duration {
	sorted := durationSlice(a)
	if !sort.IsSorted(sorted) {
		// Copy to avoid side-effects.
		sorted = durationSlice(append([]time.Duration(nil), a...))
		sort.Sort(sorted)
	}

	element := int(math.Round(float64(len(a)-1) * perc))
	return sorted[element]
}

func (wr *Routes) printCharts(w http.ResponseWriter, r *http.Request, chartMode string) {
	fmt.Fprintf(w, `<h2>Build times over time</h2><p>...for builds with at least two builds.</p>`)

	if chartMode == "rolling-average" {
		fmt.Fprintf(w, `<p>Currently displaying the rolling average (15 builds). <a href="/">Display all individual build times</a></p>`)
	} else {
		fmt.Fprintf(w, `<p>Currently displaying all builds individually. <a href="/rolling-average">Display rolling average</a></p>`)
	}

	builds, err := wr.Buildkite.ListBuilds(fromTime(r))
	if err != nil {
		// TODO: Return error.
		return
	}

	activePipelines := make(map[string]int)
	for _, b := range builds {
		name := *b.Pipeline.Name
		activePipelines[name]++
	}

	orderedList := make([]string, 0)
	for k, count := range activePipelines {
		if count <= 1 {
			continue
		}
		orderedList = append(orderedList, k)
	}
	sort.Strings(orderedList)

	for _, pipeline := range orderedList {
		fmt.Fprintf(w, `<h3>%s</h3><img src="/charts/%s/%s" />`, pipeline, url.PathEscape(pipeline), chartMode)
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
	mode := chi.URLParam(r, "mode")

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

	var ts chart.TimeSeries

	switch mode {
	case "rolling-average":
		ts = rollingAverageTs(items)
	default:
		ts = allBuildsTs(items)
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
			Range: &chart.ContinuousRange{
				Min: 0,
				Max: max(ts.YValues),
			},
		},
	}

	w.Header().Set("Content-Type", "image/png")
	if err := graph.Render(chart.PNG, w); err != nil {
		log.Println(err)
	}
}

func allBuildsTs(items []timelineDuration) chart.TimeSeries {
	allBuildsTS := chart.TimeSeries{
		Style: chart.Style{
			DotWidth: 3,
			Show:     true,
		},
	}

	for _, sample := range items {
		allBuildsTS.XValues = append(allBuildsTS.XValues, sample.When)
		allBuildsTS.YValues = append(allBuildsTS.YValues, sample.Duration.Seconds())
	}

	return allBuildsTS
}

func rollingAverageTs(items []timelineDuration) chart.TimeSeries {
	rollingAverage := ring.New(15)

	rollingAverageTS := chart.TimeSeries{
		Style: chart.Style{
			DotWidth: -1, // Don't show dots
			Show:     true,
		},
	}

	for _, sample := range items {
		// Save duration
		rollingAverage.Value = sample.Duration.Seconds()

		// Move ring to the next value
		rollingAverage = rollingAverage.Next()

		// Current average
		var currentRollingSum float64
		var currentRollingCount int

		rollingAverage.Do(func(val interface{}) {
			if val != nil {
				currentRollingSum += val.(float64)
				currentRollingCount++
			}
		})

		rollingAverageTS.XValues = append(rollingAverageTS.XValues, sample.When)
		rollingAverageTS.YValues = append(rollingAverageTS.YValues, currentRollingSum/float64(currentRollingCount))
	}

	return rollingAverageTS
}

func max(a []float64) float64 {
	var v float64
	for _, e := range a {
		if e > v {
			v = e
		}
	}
	return v
}

func nilToString(s *string) string {
	if s == nil {
		return "nil"
	}
	return *s
}

func fromTime(w *http.Request) time.Time {
	return time.Now().Add(-24 * 28 * time.Hour)
}
