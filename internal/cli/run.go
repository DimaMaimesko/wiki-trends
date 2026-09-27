package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dmytromaimesko/wiki-trends/internal/analyze"
	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/pipeline"
	"github.com/dmytromaimesko/wiki-trends/internal/render"
)

// analysisFlags are the knobs a follow-up question is most likely to turn. They
// are shared by `run` and `analyze` so that re-answering under new assumptions
// uses the identical flag names as the original call — the agent never has to
// translate between two vocabularies.
type analysisFlags struct {
	metric   string
	minDaily float64
	spikeZ   float64
	alpha    float64
	weights  string
}

func (a *analysisFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&a.metric, "metric", "share", "share|absolute — which series the verdict is based on")
	fs.Float64Var(&a.minDaily, "min-daily", 10, "minimum mean daily views before a trend claim is allowed")
	fs.Float64Var(&a.spikeZ, "spike-z", 4, "robust z threshold for event-spike detection")
	fs.Float64Var(&a.alpha, "alpha", 0.05, "significance level, applied to FDR-adjusted p-values")
	fs.StringVar(&a.weights, "weights", "", "opportunity-score weights, e.g. level=0.45,growth=0.35,reliability=0.20")
}

func (a *analysisFlags) options() (analyze.Options, error) {
	w, err := parseWeights(a.weights)
	if err != nil {
		return analyze.Options{}, err
	}
	if a.metric != "share" && a.metric != "absolute" {
		return analyze.Options{}, fmt.Errorf("--metric must be share or absolute, got %q", a.metric)
	}
	return analyze.Options{
		Metric: a.metric, MinDailyViews: a.minDaily,
		SpikeZ: a.spikeZ, Alpha: a.alpha, Weights: w,
	}, nil
}

func cmdRun(ctx context.Context, args []string, brief bool) error {
	name := "run"
	if brief {
		name = "scan"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var c common
	var af analysisFlags
	c.bind(fs)
	af.bind(fs)
	topic := fs.String("topic", "", "free-text topic")
	qid := fs.String("qid", "", "Wikidata item id (skips search; preferred)")
	basket := fs.String("basket", "", "extra Wikidata items summed into the topic, e.g. Q544,Q318")
	basketTopics := fs.String("basket-topics", "", "extra concepts by name, resolved via search, e.g. \"solar system,galaxy,black hole\"")
	titles := fs.String("titles", "", "per-edition title overrides, e.g. pl=Głodówka,cs=Přerušovaný půst")
	redirects := fs.Bool("include-redirects", false, "sum traffic arriving via redirect titles")
	out := fs.String("out", "", "output directory (default ./wikitrends-runs/<slug>)")
	title := fs.String("title", "", "report title")
	question := fs.String("question", "", "the user's question, echoed on the report")
	noReport := fs.Bool("no-report", false, "skip PDF/HTML/SVG rendering")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *topic == "" && *qid == "" && *titles == "" {
		return fmt.Errorf("%s needs --topic, --qid or --titles", name)
	}
	if len(c.wikiList()) == 0 {
		return fmt.Errorf("%s needs --wikis, e.g. --wikis pl,cs,uk", name)
	}
	// `scan` defaults to a longer window than `run`: shortlisting across many
	// editions leans on year-over-year and seasonality, both of which need more
	// than two years to be estimable at all.
	if brief && !isFlagSet(fs, "months") {
		c.months = 36
	}

	start, end, err := c.window()
	if err != nil {
		return err
	}
	cl, err := c.client()
	if err != nil {
		return err
	}
	defer cl.Close()

	fmt.Fprintf(os.Stderr, "fetching %s..%s for %d editions\n", start.Format("2006-01-02"), end.Format("2006-01-02"), len(c.wikiList()))
	ds, err := pipeline.Fetch(ctx, cl, pipeline.FetchOptions{
		Query: *topic, QID: *qid, Basket: splitList(*basket), BasketTopics: splitList(*basketTopics),
		Wikis: c.wikiList(), Titles: parseKV(*titles),
		Start: start, End: end,
		Access: c.access, Agent: c.agent, SearchLang: c.searchLang,
		IncludeRedirects: *redirects,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[cache: %d hits, %d misses, %d requests, %d retries]\n",
		cl.Stats.Hits, cl.Stats.Misses, cl.Stats.Requests, cl.Stats.Retries)

	opts, err := af.options()
	if err != nil {
		return err
	}
	an, err := analyze.Run(ds, opts)
	if err != nil {
		return err
	}

	dir := *out
	if dir == "" {
		dir = filepath.Join("wikitrends-runs", slug(ds))
	}
	rep := render.Report{Dataset: ds, Analysis: an, Title: *title, Question: *question}
	if err := writeRun(dir, ds, an, rep, !*noReport); err != nil {
		return err
	}

	if brief {
		fmt.Print(briefSummary(rep))
	} else {
		fmt.Print(rep.Markdown())
	}
	fmt.Fprintf(os.Stderr, "\nartifacts written to %s\n", dir)
	return nil
}

func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	var af analysisFlags
	af.bind(fs)
	in := fs.String("in", "", "run directory containing dataset.json")
	title := fs.String("title", "", "report title")
	question := fs.String("question", "", "the user's question, echoed on the report")
	noReport := fs.Bool("no-report", false, "skip PDF/HTML/SVG rendering")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("analyze needs --in <run directory>")
	}
	ds, err := readDataset(filepath.Join(*in, "dataset.json"))
	if err != nil {
		return err
	}
	opts, err := af.options()
	if err != nil {
		return err
	}
	an, err := analyze.Run(ds, opts)
	if err != nil {
		return err
	}
	rep := render.Report{Dataset: ds, Analysis: an, Title: *title, Question: *question}
	if err := writeRun(*in, ds, an, rep, !*noReport); err != nil {
		return err
	}
	fmt.Print(rep.Markdown())
	fmt.Fprintf(os.Stderr, "\nre-analysed from cache; artifacts updated in %s\n", *in)
	return nil
}

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	in := fs.String("in", "", "run directory containing dataset.json and analysis.json")
	title := fs.String("title", "", "report title")
	question := fs.String("question", "", "the user's question, echoed on the report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("report needs --in <run directory>")
	}
	ds, err := readDataset(filepath.Join(*in, "dataset.json"))
	if err != nil {
		return err
	}

	// The analysis is recomputed from the dataset using the options recorded in
	// analysis.json, rather than deserialised from it. analysis.json encodes
	// "not computed" as JSON null, which would silently decode back into a
	// confident 0.0 - a report claiming +0%/yr where nothing was measurable is
	// worse than no report. Recomputing costs milliseconds and cannot drift.
	opts := analyze.Options{}
	if b, err := os.ReadFile(filepath.Join(*in, "analysis.json")); err == nil {
		var prev model.Analysis
		if json.Unmarshal(b, &prev) == nil {
			opts = analyze.Options{
				Metric:        prev.Options.Metric,
				SpikePolicy:   prev.Options.SpikePolicy,
				MinDailyViews: prev.Options.MinDailyViews,
				SpikeZ:        prev.Options.SpikeZ,
				Alpha:         prev.Options.Alpha,
				Weights:       prev.Options.Weights,
			}
		}
	}
	an, err := analyze.Run(ds, opts)
	if err != nil {
		return err
	}
	rep := render.Report{Dataset: ds, Analysis: an, Title: *title, Question: *question}
	if err := writeRun(*in, ds, an, rep, true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "report re-rendered in %s\n", *in)
	return nil
}

// ---------------------------------------------------------------- artifacts

func writeRun(dir string, ds *model.Dataset, an *model.Analysis, rep render.Report, withReport bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "dataset.json"), ds); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "analysis.json"), an); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(rep.Markdown()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "series.csv"), rep.CSV(), 0o644); err != nil {
		return err
	}
	if !withReport {
		return nil
	}
	return writeArtifacts(dir, rep)
}

func writeArtifacts(dir string, rep render.Report) error {
	if err := os.WriteFile(filepath.Join(dir, "report.html"), rep.HTML(), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.pdf"), rep.PDF(), 0o644); err != nil {
		return err
	}
	for name, b := range rep.SVGCharts() {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	// model.MarshalSafe rather than encoding/json: the analysis carries NaN for
	// every statistic that could not be computed, and plain json.Marshal refuses
	// to encode it.
	b, err := model.MarshalSafe(v, " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readDataset(path string) (*model.Dataset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w (run `wikitrends run` first, or pass the right --in directory)", err)
	}
	var ds model.Dataset
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, err
	}
	if ds.Schema != model.DatasetSchema {
		return nil, fmt.Errorf("dataset schema %q, expected %q", ds.Schema, model.DatasetSchema)
	}
	return &ds, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug builds a stable directory name from the topic and editions, so a
// follow-up run lands in the same place and the agent can predict the path
// without being told.
func slug(ds *model.Dataset) string {
	base := ds.Topic.LabelEn
	if base == "" {
		base = ds.Topic.Query
	}
	if base == "" {
		base = ds.Topic.QID
	}
	s := slugRe.ReplaceAllString(strings.ToLower(base), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "topic"
	}
	var wikis []string
	for _, x := range ds.Series {
		wikis = append(wikis, x.Wiki)
	}
	if len(wikis) > 4 {
		wikis = append(wikis[:4], fmt.Sprintf("and%d", len(wikis)-4))
	}
	return s + "-" + strings.Join(wikis, "")
}

// briefSummary is the `scan` output: the shortlist and the caveats, without the
// per-edition detail. Reading twenty full edition blocks into context defeats the
// purpose of scanning twenty editions.
func briefSummary(rep render.Report) string {
	var b strings.Builder
	a := rep.Analysis
	fmt.Fprintf(&b, "# %s — %d editions, %s to %s\n\n", topicName(a), len(a.Wikis), a.Window.Start, a.Window.End)
	if c := a.Comparison; c != nil {
		fmt.Fprintf(&b, "Weights: level %.2f, growth %.2f, reliability %.2f\n\n",
			c.Weights["level"], c.Weights["growth"], c.Weights["reliability"])
		b.WriteString("| # | edition | score | per million | share %/yr | abs %/yr | wiki %/yr | YoY | adj p | grade |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
		for i, r := range c.Ranking {
			fmt.Fprintf(&b, "| %d | %s | %s | %.2f | %s | %s | %s | %s | %s | %s |\n",
				i+1, r.Wiki, fmtF(r.OpportunityScore, "%.2f"), r.LevelPerMillion,
				fmtF(r.GrowthPctPerYear, "%+.0f%%"), fmtF(r.AbsGrowthPctPerYear, "%+.0f%%"),
				fmtF(r.WikiGrowthPctPerYear, "%+.0f%%"), fmtF(r.YoYPct, "%+.0f%%"),
				render.FmtP(r.PValueAdj), r.Grade)
		}
		b.WriteString("\nshare %/yr is the topic's trend after dividing out the edition's own traffic; abs %/yr is the raw view trend; wiki %/yr is the edition's total traffic. share is the one to act on.\n")
		b.WriteString("\n")
		for _, r := range c.Ranking {
			fmt.Fprintf(&b, "- **%s**: %s\n", r.Wiki, r.Rationale)
		}
		b.WriteString("\n")
	}
	b.WriteString("Read summary.md in the run directory for the full per-edition analysis.\n\n")
	b.WriteString("## Assumptions and limitations\n\n")
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

func topicName(a *model.Analysis) string {
	if a.Topic.LabelEn != "" {
		return a.Topic.LabelEn
	}
	if a.Topic.Query != "" {
		return a.Topic.Query
	}
	return a.Topic.QID
}

func fmtF(v float64, f string) string {
	if v != v { // NaN
		return "—"
	}
	return fmt.Sprintf(f, v)
}
