package main

import (
	"container/ring"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	_ "net/http/pprof"

	"github.com/go-chi/chi"
	chart "github.com/wcharczuk/go-chart"
)

type Routes struct {
	Buildkite Buildkite
	Queries   []Query
}

func (wr *Routes) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", wr.root)
	r.Get("/{query}/", wr.report)
	r.Get("/{query}/rolling-average", wr.root)

	r.Get("/{query}/charts/{pipeline}/{mode}", wr.charts)
	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

	return r
}

func (wr *Routes) root(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/0/", 303)
}

func (wr *Routes) query(r *http.Request) (int, Query, error) {
	query := chi.URLParam(r, "query")
	i, err := strconv.Atoi(query)
	if err != nil {
		return i, Query{}, err
	}
	if i < 0 || i >= len(wr.Queries) {
		return i, Query{}, errors.New("query missing")
	}
	return i, wr.Queries[i], nil
}

func (wr *Routes) report(w http.ResponseWriter, r *http.Request) {
	queryIndex, query, err := wr.query(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	chartMode := "all"
	if r.RequestURI == "/rolling-average" {
		chartMode = "rolling-average"
	}

	wr.printTopHtml(w, r, queryIndex)
	wr.totalTopList(w, r, query)
	wr.percentileTopList(w, r, 90, query)
	wr.printCharts(w, r, chartMode, queryIndex, query)
	wr.printBottomHtml(w, r)
}

func (wr *Routes) printTopHtml(w http.ResponseWriter, r *http.Request, queryIndex int) {
	// TODO: Add navbar with report selected based on queryIndex.
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html lang="">
  <head>
    <meta charset="utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="description" content="pyramid web application">
    <link rel="shortcut icon" href="/static/favicon.ico">

    <title>Buildkite dashboard</title>

    <!-- Bootstrap core CSS -->
    <link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/3.3.7/css/bootstrap.min.css" integrity="sha384-BVYiiSIFeK1dGmJRAkycuHAHRg32OmUcww7on3RYdg4Va+PmSTsz/K68vbdEjh4u" crossorigin="anonymous">

    <!-- HTML5 shim and Respond.js IE8 support of HTML5 elements and media queries -->
    <!--[if lt IE 9]>
      <script src="//oss.maxcdn.com/libs/html5shiv/3.7.0/html5shiv.js" integrity="sha384-0s5Pv64cNZJieYFkXYOTId2HMA2Lfb6q2nAcx2n0RTLUnCAoTTsS0nKEO27XyKcY" crossorigin="anonymous"></script>
      <script src="//oss.maxcdn.com/libs/respond.js/1.3.0/respond.min.js" integrity="sha384-f1r2UzjsxZ9T4V1f2zBO/evUqSEOpeaUUZcMTz1Up63bl4ruYnFYeM+BxI4NhyI0" crossorigin="anonymous"></script>
    <![endif]-->
  </head>
  <div class="starter-template">
      <div class="container">
        <div class="row">
          <div class="col-md-12">
            <h1>Buildkite Dashboard</h1>`)
}

func (wr *Routes) printBottomHtml(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
	      </div>
        </div>
      </div>
    </div>
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

func (wr *Routes) totalTopList(w http.ResponseWriter, r *http.Request, q Query) {
	fmt.Fprintf(w, `<h2>Total time spent building staging past 4 weeks</h2>`)

	builds, err := wr.Buildkite.ListBuilds(fromTime(r), q)
	if err != nil {
		// TODO: Return error.
		return
	}

	sums := make(map[string]time.Duration)
	for _, b := range builds {
		name := b.Pipeline.Name
		sums[name] += b.FinishedAt.Sub(b.StartedAt)
	}

	sumsList := make(namedDurationSlice, 0, len(sums))
	for k, v := range sums {
		sumsList = append(sumsList, namedDuration{k, v})
	}
	sort.Sort(sort.Reverse(sumsList))

	fmt.Fprintf(w, `<table class="table table-condensed"><tr><th>Pipeline</th><th>Total Duration</th></tr>`)
	for _, pipeline := range sumsList {
		fmt.Fprintf(w, `<tr><th>%s</th><td>%s</td></tr>`, pipeline.Name, pipeline.Duration)
	}
	fmt.Fprintf(w, `</table>`)
}

func (wr *Routes) percentileTopList(w http.ResponseWriter, r *http.Request, perc int, q Query) {
	fmt.Fprintf(w, `<h2>%dth percentile of time spent building staging past 4 weeks</h2>`, perc)
	fperc := float64(perc) / 100

	builds, err := wr.Buildkite.ListBuilds(fromTime(r), q)
	if err != nil {
		// TODO: Return error.
		return
	}

	durationsByPipeline := make(map[string][]time.Duration)
	for _, b := range builds {
		name := b.Pipeline.Name
		durationsByPipeline[name] = append(durationsByPipeline[name], b.FinishedAt.Sub(b.StartedAt))
	}

	sumsList := make(namedDurationSlice, 0, len(durationsByPipeline))
	for k, v := range durationsByPipeline {
		sumsList = append(sumsList, namedDuration{k, durationPercentile(v, fperc)})
	}
	sort.Sort(sort.Reverse(sumsList))

	fmt.Fprintf(w, `<table class="table table-condensed"><tr><th>Pipeline</th><th>%dth percentile</th></tr>`, perc)
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

func (wr *Routes) printCharts(w http.ResponseWriter, r *http.Request, chartMode string, queryIndex int, q Query) {
	fmt.Fprintf(w, `<h2>Build times over time</h2><p>...for builds with at least two builds.</p>`)

	if chartMode == "rolling-average" {
		fmt.Fprintf(w, `<p>Currently displaying the rolling average (15 builds). <a href="/%d/">Display all individual build times</a></p>`, queryIndex)
	} else {
		fmt.Fprintf(w, `<p>Currently displaying all builds individually. <a href="/%d/rolling-average">Display rolling average</a></p>`, queryIndex)
	}

	builds, err := wr.Buildkite.ListBuilds(fromTime(r), q)
	if err != nil {
		// TODO: Return error.
		return
	}

	activePipelines := make(map[string]int)
	for _, b := range builds {
		name := b.Pipeline.Name
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
		fmt.Fprintf(w, `<h3>%s</h3><img src="/%d/charts/%s/%s" />`, pipeline, queryIndex, url.PathEscape(pipeline), chartMode)
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

	_, query, err := wr.query(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	builds, err := wr.Buildkite.ListBuilds(fromTime(r), query)
	if err != nil {
		// TODO: Return error.
		return
	}

	items := make(timelineSlice, 0)
	for _, b := range builds {
		name := b.Pipeline.Name
		if name != pipeline {
			continue
		}
		items = append(items, timelineDuration{b.StartedAt, b.FinishedAt.Sub(b.StartedAt)})
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
