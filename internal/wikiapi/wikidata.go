package wikiapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Resolution is the step that makes cross-language comparison legitimate.
//
// A naive implementation asks for "Astronomy" in each wiki and hopes the title
// translates. It does not: titles differ, redirects abound, and two wikis can
// use the same string for different concepts. Everything here routes through a
// Wikidata item (QID) instead, so "is interest in astronomy growing in Ukrainian
// Wikipedia?" is answered with the article that Wikidata says *is* astronomy in
// ukwiki — or with an explicit "this wiki has no article for that concept",
// which is itself a finding a product team needs.

const actionTTL = 24 * time.Hour

type Concept struct {
	QID         string
	Label       string
	Description string
	Score       int
}

// SearchConcepts finds candidate Wikidata items for a free-text topic.
//
// Two sources are merged because each fails differently. Wikidata's own search
// matches labels and aliases, so it is precise but misses descriptive phrases.
// Wikipedia's full-text search understands phrasing but returns articles, which
// then have to be mapped back to items. Results are merged with Wikipedia's
// ranking first, since users phrase topics the way articles are written.
func (c *Client) SearchConcepts(ctx context.Context, lang, query string, limit int) ([]Concept, error) {
	seen := map[string]bool{}
	var out []Concept

	if titles, err := c.SearchTitles(ctx, lang+".wikipedia", query, limit); err == nil {
		names := make([]string, 0, len(titles))
		for _, t := range titles {
			names = append(names, t.Title)
		}
		if qids, err := c.QIDsForTitles(ctx, lang+".wikipedia", names); err == nil {
			for _, t := range titles {
				q := qids[t.Title]
				if q == "" || seen[q] {
					continue
				}
				seen[q] = true
				out = append(out, Concept{QID: q, Label: t.Title, Description: t.Snippet, Score: t.Size})
			}
		}
	}

	var r struct {
		Search []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"search"`
	}
	u := "https://www.wikidata.org/w/api.php?action=wbsearchentities&format=json&type=item&limit=" +
		fmt.Sprint(limit) + "&language=" + url.QueryEscape(lang) + "&uselang=" + url.QueryEscape(lang) +
		"&search=" + url.QueryEscape(query)
	if err := c.getJSON(ctx, u, actionTTL, &r); err == nil {
		for _, s := range r.Search {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			out = append(out, Concept{QID: s.ID, Label: s.Label, Description: s.Description})
		}
	}
	if len(out) == 0 {
		return nil, ErrNoData
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type Entity struct {
	QID           string
	LabelEn       string
	DescriptionEn string
	Sitelinks     map[string]string // wiki language code -> article title
}

// Entity fetches an item's English label/description and every language
// edition's article title. A wiki missing from Sitelinks genuinely has no
// article for the concept.
//
// The request deliberately does NOT use the API's sitefilter parameter, even
// though it would shrink the response from about 3 KB to 350 bytes. Filtering
// server-side would put the requested edition list into the cache key, so
// "now also add German" would miss the cache and re-request a fact that had
// already been fetched. Since adding editions is the most common follow-up in
// this skill, one reusable entry beats nine small unreusable ones. Filtering
// happens client-side in Sitelinks lookups instead.
func (c *Client) Entity(ctx context.Context, qid string, wikis []string) (*Entity, error) {
	u := "https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&props=sitelinks%7Clabels%7Cdescriptions&languages=en&ids=" +
		url.QueryEscape(qid)
	var r struct {
		Entities map[string]struct {
			Labels map[string]struct {
				Value string `json:"value"`
			} `json:"labels"`
			Descriptions map[string]struct {
				Value string `json:"value"`
			} `json:"descriptions"`
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
			Missing any `json:"missing"`
		} `json:"entities"`
	}
	if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
		return nil, err
	}
	e, ok := r.Entities[qid]
	if !ok || e.Missing != nil {
		return nil, fmt.Errorf("wikidata item %s not found", qid)
	}
	out := &Entity{QID: qid, Sitelinks: map[string]string{}}
	out.LabelEn = e.Labels["en"].Value
	out.DescriptionEn = e.Descriptions["en"].Value
	for site, sl := range e.Sitelinks {
		if lang, ok := langOf(site); ok {
			out.Sitelinks[lang] = sl.Title
		}
	}
	return out, nil
}

func langOf(site string) (string, bool) {
	if !strings.HasSuffix(site, "wiki") {
		return "", false
	}
	base := strings.TrimSuffix(site, "wiki")
	// exclude sister projects such as "commonswiki", "specieswiki"
	if base == "" || base == "commons" || base == "species" || base == "meta" || base == "wikidata" {
		return "", false
	}
	if strings.ContainsAny(base, "0123456789") {
		return "", false
	}
	return strings.ReplaceAll(base, "_", "-"), true
}

type TitleHit struct {
	Title   string
	Snippet string
	Size    int
}

// SearchTitles runs a full-text search inside one wiki. It is the fallback path
// when Wikidata has no item for a phrasing, and the tool the agent uses to
// propose a native-language title for a wiki where the concept has no sitelink.
func (c *Client) SearchTitles(ctx context.Context, project, query string, limit int) ([]TitleHit, error) {
	u := apiBase(project) + "?action=query&format=json&list=search&srlimit=" + fmt.Sprint(limit) +
		"&srsearch=" + url.QueryEscape(query)
	var r struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				Size    int    `json:"size"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
		return nil, err
	}
	out := make([]TitleHit, 0, len(r.Query.Search))
	for _, s := range r.Query.Search {
		out = append(out, TitleHit{Title: s.Title, Snippet: stripTags(s.Snippet), Size: s.Size})
	}
	if len(out) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}

// PageInfo is what the skill needs to know about an article before trusting its
// pageview series.
type PageInfo struct {
	Title    string // canonical title after normalisation and redirect resolution
	Given    string // what was asked for
	QID      string
	Missing  bool
	Redirect bool // the requested title was a redirect
	URL      string
	Created  string // first revision date, YYYY-MM-DD
}

// ResolveTitle normalises a title and follows redirects.
//
// This matters because pageviews are counted per *title*. If the user names a
// redirect, its own view count is only the people who reached the article via
// that exact alias — typically a small and unstable fraction. Following the
// redirect first is the difference between measuring a topic and measuring a
// synonym.
func (c *Client) ResolveTitle(ctx context.Context, project, title string) (*PageInfo, error) {
	u := apiBase(project) + "?action=query&format=json&redirects=1&prop=pageprops&ppprop=wikibase_item&titles=" +
		url.QueryEscape(title)
	var r struct {
		Query struct {
			Redirects []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"redirects"`
			Pages map[string]struct {
				Title     string `json:"title"`
				Missing   any    `json:"missing"`
				PageProps struct {
					WikibaseItem string `json:"wikibase_item"`
				} `json:"pageprops"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
		return nil, err
	}
	for _, p := range r.Query.Pages {
		info := &PageInfo{Given: title, Title: p.Title, QID: p.PageProps.WikibaseItem, Missing: p.Missing != nil}
		info.Redirect = len(r.Query.Redirects) > 0
		if !info.Missing {
			info.URL = "https://" + hostOf(project) + "/wiki/" + strings.ReplaceAll(url.PathEscape(strings.ReplaceAll(p.Title, " ", "_")), "/", "%2F")
		}
		return info, nil
	}
	return nil, ErrNoData
}

// QIDsForTitles batches a title -> Wikidata item lookup (50 per request, the
// Action API limit for anonymous clients).
func (c *Client) QIDsForTitles(ctx context.Context, project string, titles []string) (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(titles); i += 50 {
		j := i + 50
		if j > len(titles) {
			j = len(titles)
		}
		u := apiBase(project) + "?action=query&format=json&redirects=1&prop=pageprops&ppprop=wikibase_item&titles=" +
			url.QueryEscape(strings.Join(titles[i:j], "|"))
		var r struct {
			Query struct {
				Normalized []struct{ From, To string } `json:"normalized"`
				Pages      map[string]struct {
					Title     string `json:"title"`
					PageProps struct {
						WikibaseItem string `json:"wikibase_item"`
					} `json:"pageprops"`
				} `json:"pages"`
			} `json:"query"`
		}
		if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
			return out, err
		}
		for _, p := range r.Query.Pages {
			if p.PageProps.WikibaseItem != "" {
				out[p.Title] = p.PageProps.WikibaseItem
			}
		}
	}
	return out, nil
}

// PageCreated returns the date of an article's first revision.
//
// Without it, a young article looks like explosive growth: the series starts at
// zero simply because the page did not exist, and a log-linear fit reads that as
// demand appearing from nothing. Knowing the creation date lets the analysis
// mark pre-creation days as "not applicable" rather than "no interest", and
// refuse a trend claim when the article is younger than the window.
func (c *Client) PageCreated(ctx context.Context, project, title string) (string, error) {
	u := apiBase(project) + "?action=query&format=json&prop=revisions&rvlimit=1&rvdir=newer&rvprop=timestamp&titles=" +
		url.QueryEscape(title)
	var r struct {
		Query struct {
			Pages map[string]struct {
				Revisions []struct {
					Timestamp string `json:"timestamp"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
		return "", err
	}
	for _, p := range r.Query.Pages {
		if len(p.Revisions) > 0 && len(p.Revisions[0].Timestamp) >= 10 {
			return p.Revisions[0].Timestamp[:10], nil
		}
	}
	return "", ErrNoData
}

// Redirects lists titles that redirect to an article.
//
// Opt-in, because summing them is a judgement call rather than a strict
// improvement: it captures readers who arrive via an alias (often a large share
// for medical and pop-culture topics) but it can also fold in a merged former
// article whose history is unrelated. The skill fetches them on request and
// always records which titles were summed.
func (c *Client) Redirects(ctx context.Context, project, title string) ([]string, error) {
	u := apiBase(project) + "?action=query&format=json&prop=redirects&rdlimit=200&rdnamespace=0&titles=" +
		url.QueryEscape(title)
	var r struct {
		Query struct {
			Pages map[string]struct {
				Redirects []struct {
					Title string `json:"title"`
				} `json:"redirects"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.getJSON(ctx, u, actionTTL, &r); err != nil {
		return nil, err
	}
	var out []string
	for _, p := range r.Query.Pages {
		for _, rd := range p.Redirects {
			out = append(out, rd.Title)
		}
	}
	return out, nil
}

func hostOf(project string) string {
	if strings.Contains(project, ".") {
		return project + ".org"
	}
	return project + ".wikipedia.org"
}

func apiBase(project string) string { return "https://" + hostOf(project) + "/w/api.php" }

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ProjectOf maps a language code to a pageviews project id.
func ProjectOf(lang string) string {
	if strings.Contains(lang, ".") {
		return lang
	}
	return lang + ".wikipedia"
}
