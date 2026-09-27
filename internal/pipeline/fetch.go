// Package pipeline wires resolution and fetching into the dataset the analysis
// stage consumes. It is deliberately separate from the CLI so the whole flow can
// be exercised in tests against a pre-seeded cache.
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dmytromaimesko/wiki-trends/internal/model"
	"github.com/dmytromaimesko/wiki-trends/internal/tseries"
	"github.com/dmytromaimesko/wiki-trends/internal/wikiapi"
)

type FetchOptions struct {
	Query  string   // free-text topic, used for search and fallbacks
	QID    string   // resolved Wikidata item; empty means "search for it"
	Basket []string // additional QIDs summed into the same topic
	// BasketTopics are free-text concept names resolved to QIDs via search. This
	// exists because an agent should never have to recall Wikidata ids from
	// memory: Q11405 is "flute", not the astronomy article it might look like,
	// and a mistyped id produces a confident chart of the wrong subject.
	BasketTopics     []string
	Wikis            []string          // language codes
	Titles           map[string]string // per-wiki title overrides (wiki -> title)
	Start            time.Time
	End              time.Time
	Access           string
	Agent            string
	SearchLang       string
	IncludeRedirects bool
}

func (o *FetchOptions) defaults() {
	if o.Access == "" {
		o.Access = "all-access"
	}
	if o.Agent == "" {
		// "user" is Wikimedia's best-effort human traffic filter. Using
		// "all-agents" would fold in crawlers, which have grown enormously and
		// unevenly across wikis; a trend measured on all-agents is substantially
		// a trend in scraping.
		o.Agent = "user"
	}
	if o.SearchLang == "" {
		o.SearchLang = "en"
	}
}

func Fetch(ctx context.Context, c *wikiapi.Client, o FetchOptions) (*model.Dataset, error) {
	o.defaults()
	if len(o.Wikis) == 0 {
		return nil, fmt.Errorf("no language editions requested")
	}

	start, end, notes := tseries.ClampWindow(o.Start, o.End, wikiapi.PageviewsStart, time.Now().UTC())
	if !end.After(start) {
		return nil, fmt.Errorf("empty window after clamping: %s..%s", start.Format(tseries.DateFmt), end.Format(tseries.DateFmt))
	}

	ds := &model.Dataset{
		Schema:    model.DatasetSchema,
		Tool:      "wikitrends",
		FetchedAt: time.Now().UTC(),
		Window: model.Window{
			Start: start.Format(tseries.DateFmt), End: end.Format(tseries.DateFmt), Granularity: "daily",
		},
		Params: model.FetchParams{Access: o.Access, Agent: o.Agent, IncludeRedirects: o.IncludeRedirects},
		Topic:  model.Topic{Query: o.Query, QID: o.QID},
		Notes:  notes,
	}

	// ---- resolve the concept, unless the caller supplied explicit titles
	var qids []string
	if o.QID != "" {
		qids = append(qids, o.QID)
	} else if len(o.Titles) == 0 && o.Query != "" {
		cands, err := c.SearchConcepts(ctx, o.SearchLang, o.Query, 5)
		if err != nil || len(cands) == 0 {
			return nil, fmt.Errorf("could not resolve topic %q to a Wikidata item; run `wikitrends resolve` and pass --qid", o.Query)
		}
		qids = append(qids, cands[0].QID)
		ds.Topic.QID = cands[0].QID
		ds.Notes = append(ds.Notes, fmt.Sprintf(
			"topic auto-resolved to %s (%s - %s); confirm this is the intended concept. Other candidates: %s",
			cands[0].QID, cands[0].Label, cands[0].Description, briefCandidates(cands[1:])))
	}
	qids = append(qids, o.Basket...)
	for _, bt := range o.BasketTopics {
		cands, err := c.SearchConcepts(ctx, o.SearchLang, bt, 3)
		if err != nil || len(cands) == 0 {
			ds.Notes = append(ds.Notes, fmt.Sprintf("basket topic %q could not be resolved and was skipped", bt))
			continue
		}
		qids = append(qids, cands[0].QID)
		ds.Notes = append(ds.Notes, fmt.Sprintf("basket topic %q resolved to %s (%s)", bt, cands[0].QID, cands[0].Label))
	}

	// ---- sitelinks: which wikis actually have an article for this concept
	titlesByWiki := map[string]map[string]string{} // wiki -> qid -> title
	for _, q := range qids {
		ent, err := c.Entity(ctx, q, o.Wikis)
		if err != nil {
			return nil, fmt.Errorf("wikidata %s: %w", q, err)
		}
		if q == ds.Topic.QID {
			ds.Topic.LabelEn, ds.Topic.DescriptionEn = ent.LabelEn, ent.DescriptionEn
		} else {
			ds.Topic.Basket = append(ds.Topic.Basket, model.BasketItem{QID: q, LabelEn: ent.LabelEn})
		}
		for w, t := range ent.Sitelinks {
			if titlesByWiki[w] == nil {
				titlesByWiki[w] = map[string]string{}
			}
			titlesByWiki[w][q] = t
		}
	}
	for w, t := range o.Titles { // explicit overrides win
		if titlesByWiki[w] == nil {
			titlesByWiki[w] = map[string]string{}
		}
		titlesByWiki[w]["override"] = t
	}

	// ---- per-wiki fetch, in parallel; the client's own limiter keeps it polite
	out := make([]model.Series, len(o.Wikis))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i, w := range o.Wikis {
		wg.Add(1)
		go func(i int, wiki string) {
			defer wg.Done()
			s, err := fetchWiki(ctx, c, o, wiki, titlesByWiki[wiki], start, end)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", wiki, err)
				}
				mu.Unlock()
				s = model.Series{Wiki: wiki, Project: wikiapi.ProjectOf(wiki), Status: model.StatusNoData, Note: err.Error()}
			}
			out[i] = s
		}(i, w)
	}
	wg.Wait()
	ds.Series = out

	for _, s := range ds.Series {
		if s.Status == model.StatusNoArticle {
			ds.Notes = append(ds.Notes, fmt.Sprintf(
				"%s.wikipedia has no article for this concept - a coverage gap, not zero interest", s.Wiki))
		}
	}
	if firstErr != nil && allFailed(ds.Series) {
		return nil, firstErr
	}
	return ds, nil
}

func allFailed(ss []model.Series) bool {
	for _, s := range ss {
		if s.Status == model.StatusOK {
			return false
		}
	}
	return true
}

func fetchWiki(ctx context.Context, c *wikiapi.Client, o FetchOptions, wiki string, byQID map[string]string, start, end time.Time) (model.Series, error) {
	project := wikiapi.ProjectOf(wiki)
	s := model.Series{Wiki: wiki, Project: project, Start: start.Format(tseries.DateFmt)}

	if len(byQID) == 0 {
		// A missing sitelink is a real answer: this language edition does not
		// cover the concept at all. Offer search hits so the agent can either
		// propose a native-language title or report the gap with evidence.
		s.Status = model.StatusNoArticle
		s.Note = "no article linked to this Wikidata item in this language edition"
		if hits, err := c.SearchTitles(ctx, project, o.Query, 5); err == nil {
			for _, h := range hits {
				s.Candidates = append(s.Candidates, model.Candidate{Title: h.Title, Snippet: h.Snippet, Size: h.Size})
			}
		}
		return s, nil
	}

	// The project denominator is fetched once per wiki and shared by every
	// article in the basket.
	proj, err := c.ProjectViews(ctx, project, o.Access, o.Agent, start, end)
	if err != nil {
		return s, fmt.Errorf("project pageviews: %w", err)
	}

	var summedViews, projArr []float64
	var cov model.Coverage

	titles := make([]string, 0, len(byQID))
	qidOf := map[string]string{}
	for q, t := range byQID {
		titles = append(titles, t)
		qidOf[t] = q
	}
	sort.Strings(titles) // deterministic output ordering

	for _, title := range titles {
		info, err := c.ResolveTitle(ctx, project, title)
		if err != nil || info == nil || info.Missing {
			continue
		}
		art := model.Article{Title: info.Title, QID: qidOf[title], URL: info.URL}
		if art.QID == "override" {
			// A title override still gets its real Wikidata id recorded, so the
			// artifact says which concept was actually measured rather than
			// "override" - important when the override exists precisely because
			// the intended concept has no article here.
			art.QID = info.QID
		}
		if created, err := c.PageCreated(ctx, project, info.Title); err == nil {
			art.CreatedAt = created
		}

		names := []string{info.Title}
		if o.IncludeRedirects {
			if rs, err := c.Redirects(ctx, project, info.Title); err == nil {
				art.Redirects = rs
				names = append(names, rs...)
			}
		}

		merged := map[string]float64{}
		any := false
		for _, nm := range names {
			av, err := c.ArticleViews(ctx, project, nm, o.Access, o.Agent, start, end)
			if err != nil {
				continue // a redirect with no traffic simply contributes nothing
			}
			any = true
			for k, v := range av {
				merged[k] += v
			}
		}
		if !any {
			continue
		}

		views, pa, cv := tseries.Reconcile(start, end, merged, proj, art.CreatedAt)
		projArr = pa
		summedViews = tseries.Add(summedViews, views)
		cov = mergeCoverage(cov, cv)
		s.Articles = append(s.Articles, art)
	}

	if summedViews == nil {
		s.Status = model.StatusNoData
		s.Note = "article exists but the pageviews API returned no data for this window"
		return s, nil
	}

	s.Views = model.Nums(summedViews)
	s.ProjectViews = model.Nums(projArr)
	s.Coverage = cov
	s.Status = model.StatusOK

	// An article younger than most of the window cannot support a trend claim:
	// the early part of the series measures the article's existence, not demand.
	if cov.Days > 0 && float64(cov.BeforeCreate)/float64(cov.Days) > 0.5 {
		s.Status = model.StatusTooNew
		s.Note = fmt.Sprintf("article created %s, after more than half of the requested window", s.Articles[0].CreatedAt)
	}
	return s, nil
}

// mergeCoverage combines per-article coverage within a basket. Observation
// counts are maxed rather than summed so completeness stays a fraction of days
// rather than of article-days.
func mergeCoverage(a, b model.Coverage) model.Coverage {
	if a.Days == 0 {
		return b
	}
	out := a
	out.Observed = maxi(a.Observed, b.Observed)
	out.ZeroFilled = maxi(a.ZeroFilled, b.ZeroFilled)
	out.Gaps = maxi(a.Gaps, b.Gaps)
	out.BeforeCreate = mini(a.BeforeCreate, b.BeforeCreate) // the basket exists once any member does
	if eligible := out.Days - out.BeforeCreate; eligible > 0 {
		out.Completeness = float64(out.Observed+out.ZeroFilled) / float64(eligible)
		if out.Completeness > 1 {
			out.Completeness = 1
		}
	}
	return out
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func briefCandidates(cs []wikiapi.Concept) string {
	if len(cs) == 0 {
		return "none"
	}
	out := ""
	for i, c := range cs {
		if i > 0 {
			out += ", "
		}
		out += c.QID + " (" + c.Label + ")"
	}
	return out
}
