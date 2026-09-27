// Package cli implements the wikitrends command line.
//
// The command surface is shaped around how an agent actually uses it across a
// conversation, not around the pipeline's internals:
//
//	resolve   answer "which article is this, in which editions does it exist"
//	          before any numbers are produced, so an ambiguous topic becomes a
//	          question to the user instead of a silently wrong comparison
//	run       the whole pipeline for a first answer
//	analyze   re-answer under different assumptions, from cached data
//	report    re-render the artifacts without recomputing
//	scan      the same as run, tuned for many editions at once
//
// The split between run / analyze / report is the single most important piece of
// ergonomics here. Follow-up questions in this domain are almost always about
// assumptions ("exclude the spike", "use absolute views", "weight audience size
// higher", "make it three years"), and each of those must be a fast local
// re-run, not a fresh download.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
	"github.com/dmytromaimesko/wiki-trends/internal/wikiapi"
)

const Version = "1.0.0"

func Main(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	cmd, rest := args[0], args[1:]
	ctx := context.Background()
	var err error
	switch cmd {
	case "resolve":
		err = cmdResolve(ctx, rest)
	case "run":
		err = cmdRun(ctx, rest, false)
	case "scan":
		err = cmdRun(ctx, rest, true)
	case "analyze":
		err = cmdAnalyze(rest)
	case "report":
		err = cmdReport(rest)
	case "cache":
		err = cmdCache(rest)
	case "doctor":
		err = cmdDoctor(ctx, rest)
	case "version", "--version", "-v":
		fmt.Println("wikitrends", Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `wikitrends — Wikipedia pageview research for product decisions

  wikitrends resolve --topic "intermittent fasting" --wikis pl,cs,uk
      Find the Wikidata concept and the article title in each language edition.
      Run this first for any new topic: it reveals ambiguity and coverage gaps
      before any trend is computed.

  wikitrends run --qid Q1666254 --wikis cs,uk --months 24 --out DIR
  wikitrends run --topic "astronomy" --wikis uk --months 36
      Fetch, analyse and render. Writes dataset.json, analysis.json, summary.md,
      report.html, report.pdf, series.csv and SVG charts; prints summary.md.

  wikitrends scan --topic "English language" --wikis pl,cs,uk,de,es,tr --months 36
      Same pipeline, compact output, tuned for shortlisting many editions.

  wikitrends analyze --in DIR [--metric absolute] [--weights level=0.6,growth=0.2,reliability=0.2]
      Re-answer from the cached dataset under different assumptions. No network.

  wikitrends report --in DIR [--title "..."] [--question "..."]
      Re-render the report from analysis.json. No network.

  wikitrends cache stats | clear
  wikitrends doctor        Check connectivity, cache and API reachability.

Common flags:
  --months N          window length, counted back from the last final day (default 24)
  --start, --end      explicit YYYY-MM-DD window (overrides --months)
  --wikis a,b,c       language edition codes
  --titles pl=Foo     per-edition article title overrides, comma separated
  --basket Q1,Q2      extra Wikidata items summed into the same topic
  --basket-topics "a,b"  same, but by name - resolved via search, so no ids to recall
  --include-redirects sum traffic arriving via redirect titles
  --metric share|absolute        default share (per million project pageviews)
  --min-daily N       below this mean daily views, no trend claim is made (default 10)
  --access, --agent   pageviews API filters (default all-access, user)
  --offline           use only cached responses; fail rather than fetch
  --json              machine-readable output where applicable
`)
}

// ---------------------------------------------------------------- shared flags

type common struct {
	wikis      string
	months     int
	start      string
	end        string
	access     string
	agent      string
	offline    bool
	cacheDir   string
	searchLang string
}

func (c *common) bind(fs *flag.FlagSet) {
	fs.StringVar(&c.wikis, "wikis", "", "comma-separated language edition codes, e.g. pl,cs,uk")
	fs.IntVar(&c.months, "months", 24, "window length in months")
	fs.StringVar(&c.start, "start", "", "window start YYYY-MM-DD")
	fs.StringVar(&c.end, "end", "", "window end YYYY-MM-DD")
	fs.StringVar(&c.access, "access", "all-access", "all-access|desktop|mobile-web|mobile-app")
	fs.StringVar(&c.agent, "agent", "user", "user|all-agents|spider|automated")
	fs.BoolVar(&c.offline, "offline", false, "use only cached API responses")
	fs.StringVar(&c.cacheDir, "cache-dir", "", "override the response cache directory")
	fs.StringVar(&c.searchLang, "search-lang", "en", "language used for concept search")
}

func (c *common) window() (time.Time, time.Time, error) {
	// The end defaults to the last day Wikimedia has certainly finalised, not to
	// today: including a partial day reads as a sudden collapse in the series.
	end := time.Now().UTC().AddDate(0, 0, -2)
	if c.end != "" {
		t, err := tseries.ParseDate(c.end)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = t
	}
	start := end.AddDate(0, -c.months, 0)
	if c.start != "" {
		t, err := tseries.ParseDate(c.start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start = t
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("window end %s is not after start %s", end.Format(tseries.DateFmt), start.Format(tseries.DateFmt))
	}
	return start, end, nil
}

func (c *common) client() (*wikiapi.Client, error) {
	cache, err := wikiapi.OpenCache(c.cacheDir)
	if err != nil {
		return nil, err
	}
	return wikiapi.New(cache, wikiapi.WithOffline(c.offline)), nil
}

func (c *common) wikiList() []string { return splitList(c.wikis) }

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseKV(s string) map[string]string {
	out := map[string]string{}
	for _, p := range splitList(s) {
		if i := strings.IndexByte(p, '='); i > 0 {
			out[strings.TrimSpace(p[:i])] = strings.TrimSpace(p[i+1:])
		}
	}
	return out
}

func parseWeights(s string) (map[string]float64, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]float64{}
	for k, v := range parseKV(s) {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("bad weight %q: %w", k, err)
		}
		out[k] = f
	}
	for _, k := range []string{"level", "growth", "reliability"} {
		if _, ok := out[k]; !ok {
			return nil, fmt.Errorf("weights must include level, growth and reliability; missing %q", k)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- resolve

func cmdResolve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	var c common
	c.bind(fs)
	topic := fs.String("topic", "", "free-text topic to resolve")
	qid := fs.String("qid", "", "resolve sitelinks for a known Wikidata item instead of searching")
	limit := fs.Int("limit", 6, "number of candidate concepts to show")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *topic == "" && *qid == "" {
		return fmt.Errorf("resolve needs --topic or --qid")
	}
	cl, err := c.client()
	if err != nil {
		return err
	}
	defer cl.Close()

	type wikiRow struct {
		Wiki       string             `json:"wiki"`
		Title      string             `json:"title,omitempty"`
		URL        string             `json:"url,omitempty"`
		Created    string             `json:"created,omitempty"`
		Status     string             `json:"status"`
		Candidates []wikiapi.TitleHit `json:"search_candidates,omitempty"`
	}
	type result struct {
		Query      string            `json:"query,omitempty"`
		Candidates []wikiapi.Concept `json:"concept_candidates,omitempty"`
		Chosen     string            `json:"chosen_qid,omitempty"`
		LabelEn    string            `json:"label_en,omitempty"`
		DescEn     string            `json:"description_en,omitempty"`
		Editions   []wikiRow         `json:"editions,omitempty"`
	}
	res := result{Query: *topic}

	chosen := *qid
	if chosen == "" {
		cands, err := cl.SearchConcepts(ctx, c.searchLang, *topic, *limit)
		if err != nil {
			return fmt.Errorf("no Wikidata concept matched %q: %w", *topic, err)
		}
		res.Candidates = cands
		chosen = cands[0].QID
	}
	res.Chosen = chosen

	wikis := c.wikiList()
	if len(wikis) > 0 {
		ent, err := cl.Entity(ctx, chosen, wikis)
		if err != nil {
			return err
		}
		res.LabelEn, res.DescEn = ent.LabelEn, ent.DescriptionEn
		for _, w := range wikis {
			row := wikiRow{Wiki: w}
			title, ok := ent.Sitelinks[w]
			if !ok {
				row.Status = "no_article"
				q := *topic
				if q == "" {
					q = ent.LabelEn
				}
				if hits, err := cl.SearchTitles(ctx, wikiapi.ProjectOf(w), q, 5); err == nil {
					row.Candidates = hits
				}
				res.Editions = append(res.Editions, row)
				continue
			}
			row.Status = "ok"
			row.Title = title
			if info, err := cl.ResolveTitle(ctx, wikiapi.ProjectOf(w), title); err == nil && !info.Missing {
				row.Title, row.URL = info.Title, info.URL
			}
			if cr, err := cl.PageCreated(ctx, wikiapi.ProjectOf(w), row.Title); err == nil {
				row.Created = cr
			}
			res.Editions = append(res.Editions, row)
		}
	}

	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(res)
	}
	if len(res.Candidates) > 0 {
		fmt.Printf("Concept candidates for %q (best first):\n", *topic)
		for i, cd := range res.Candidates {
			mark := " "
			if i == 0 {
				mark = "*"
			}
			fmt.Printf("  %s %-12s %s — %s\n", mark, cd.QID, cd.Label, cd.Description)
		}
		fmt.Println("\n  * = used below. If it is the wrong concept, re-run with --qid <other>.")
	}
	if res.LabelEn != "" {
		fmt.Printf("\nUsing %s (%s): %s\n", res.Chosen, res.LabelEn, res.DescEn)
	}
	if len(res.Editions) > 0 {
		fmt.Println("\nArticle per language edition:")
		for _, e := range res.Editions {
			if e.Status == "ok" {
				fmt.Printf("  %-6s %s  (created %s)\n", e.Wiki, e.Title, orDash(e.Created))
				continue
			}
			fmt.Printf("  %-6s NO ARTICLE for this concept\n", e.Wiki)
			for _, h := range e.Candidates {
				fmt.Printf("           candidate: %s\n", h.Title)
			}
			fmt.Printf("           -> this is a coverage gap, not zero interest. Either pick a candidate with\n")
			fmt.Printf("              --titles %s=<title>, or report the gap.\n", e.Wiki)
		}
	}
	fmt.Fprintf(os.Stderr, "\n[cache: %d hits, %d misses, %d requests]\n", cl.Stats.Hits, cl.Stats.Misses, cl.Stats.Requests)
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
