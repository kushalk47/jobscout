package linkedin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"

	"github.com/kushalk47/jobscout/internal/jobs"
)

// Client implements jobs.Source on top of the shared Chrome tab.
type Client struct {
	browser  *Browser
	sel      Selectors
	maxPages int
}

var _ jobs.Source = (*Client)(nil)

func NewClient(b *Browser, sel Selectors, maxPages int) *Client {
	if maxPages < 1 {
		maxPages = 1
	}
	return &Client{browser: b, sel: sel, maxPages: maxPages}
}

type searchSnapshot struct {
	Header          string   `json:"header"`
	Cards           []string `json:"cards"`
	NoResults       bool     `json:"noResults"`
	Recommendations bool     `json:"recommendations"`
	Shells          int      `json:"shells"`
}

// Search runs one keyword query over up to maxPages result pages.
func (c *Client) Search(ctx context.Context, q jobs.SearchQuery) ([]jobs.Card, error) {
	limit := q.MaxResults
	if limit <= 0 {
		limit = pageSize * c.maxPages
	}
	var out []jobs.Card
	seen := map[string]bool{}
	err := c.browser.with(ctx, func(tab context.Context) error {
		for page := 0; page < c.maxPages && len(out) < limit; page++ {
			u, err := SearchURL(q, page)
			if err != nil {
				return err
			}
			if err := c.browser.navigate(ctx, tab, u); err != nil {
				return err
			}
			snap, err := eval[searchSnapshot](ctx, tab, searchScript(c.sel))
			if err != nil {
				return fmt.Errorf("reading search results: %w", err)
			}
			if snap.NoResults || snap.Recommendations {
				return nil
			}
			cards, err := ParseSearchCards(snap.Cards, c.sel)
			if err != nil {
				return err
			}
			if snap.Shells == 0 && page == 0 {
				return fmt.Errorf("%w: search page showed no result cards and no empty-results banner", ErrDrift)
			}
			added := 0
			for _, card := range cards {
				if seen[card.ID] || len(out) >= limit {
					continue
				}
				seen[card.ID] = true
				out = append(out, card)
				added++
			}
			if snap.Shells < pageSize-5 || added == 0 {
				return nil // last page
			}
		}
		return nil
	})
	return out, err
}

type detailSnapshot struct {
	Title string `json:"title"`
	Main  string `json:"main"`
	Found bool   `json:"found"`
}

// Get reads one job detail page.
func (c *Client) Get(ctx context.Context, id string) (jobs.Detail, error) {
	if !ValidJobID(id) {
		return jobs.Detail{}, fmt.Errorf("invalid job id %q", id)
	}
	var d jobs.Detail
	err := c.browser.with(ctx, func(tab context.Context) error {
		if err := c.browser.navigate(ctx, tab, JobURL(id)); err != nil {
			return err
		}
		snap, err := eval[detailSnapshot](ctx, tab, detailScript(c.sel))
		if err != nil {
			return fmt.Errorf("reading job %s: %w", id, err)
		}
		if !snap.Found {
			return fmt.Errorf("job %s: no description on the page (closed, removed, or %w)", id, ErrDrift)
		}
		page := "<html><head><title>" + html.EscapeString(snap.Title) + "</title></head><body>" + snap.Main + "</body></html>"
		d, err = ParseDetail(page, id, c.sel)
		return err
	})
	return d, err
}

// ---- in-page scripts --------------------------------------------------------
// The scripts only wait, scroll, click "more" and copy HTML. All field
// extraction happens in Go (parse.go) so it can be tested without a browser.

func jsJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func searchScript(s Selectors) string {
	cfg := jsJSON(map[string]any{
		"card": s.SearchCard, "idAttr": s.SearchCardIDAttr, "title": s.SearchTitle,
		"header": s.SearchHeader, "noResults": s.SearchNoResults,
	})
	return `(async (cfg) => {
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const one = (root, sels) => { for (const s of sels) { try { const e = root.querySelector(s); if (e) return e; } catch (_) {} } return null; };
  const all = () => { for (const s of cfg.card) { try { const l = document.querySelectorAll(s); if (l.length) return Array.from(l); } catch (_) {} } return []; };
  const idOf = el => { for (const a of cfg.idAttr) { const v = el.getAttribute(a) || el.querySelector('[' + a + ']')?.getAttribute(a); if (v) return v; } return null; };
  const hasTitle = el => { const t = one(el, cfg.title); return !!(t && t.textContent.trim()); };
  let cards = [];
  for (let i = 0; i < 60; i++) {
    cards = all();
    if (cards.length || one(document, cfg.noResults)) break;
    await sleep(250);
  }
  const header = (one(document, cfg.header)?.innerText || '').trim();
  const low = header.toLowerCase();
  const result = { header, cards: [], shells: cards.length,
    noResults: !!one(document, cfg.noResults) || /^no (matching )?jobs? found|^0 results/.test(low),
    recommendations: low.includes('jobs you may be interested in') };
  if (!cards.length || result.noResults || result.recommendations) return result;
  const out = new Map();
  for (const id of cards.map(idOf).filter(Boolean)) {
    if (out.has(id)) continue;
    for (let t = 0; t < 12; t++) {
      const el = all().find(e => idOf(e) === id);
      if (!el) break;
      el.scrollIntoView({ block: 'center' });
      if (hasTitle(el)) { out.set(id, el.outerHTML); break; }
      await sleep(t === 0 ? 60 : 200);
    }
  }
  result.shells = Math.max(result.shells, all().length);
  result.cards = Array.from(out.values());
  return result;
})(` + cfg + `)`
}

func detailScript(s Selectors) string {
	cfg := jsJSON(map[string]any{"desc": s.DetailDescription, "expand": s.DetailExpand})
	return `(async (cfg) => {
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const find = () => { for (const s of cfg.desc) { try { const e = document.querySelector('main ' + s) || document.querySelector(s); if (e && e.textContent.trim()) return e; } catch (_) {} } return null; };
  let box = null;
  for (let i = 0; i < 60 && !box; i++) { box = find(); if (!box) await sleep(250); }
  if (box) {
    for (const s of cfg.expand) {
      try { document.querySelectorAll(s).forEach(b => { try { (b.querySelector('span:last-child') || b).click(); } catch (_) {} }); } catch (_) {}
    }
    // The box first renders partial text and fills in during hydration:
    // wait until its length is stable for ~1s (max ~6s) before reading.
    let last = -1, stable = 0;
    for (let i = 0; i < 24 && stable < 4; i++) {
      await sleep(250);
      const n = (find() || box).textContent.length;
      stable = n === last ? stable + 1 : 0;
      last = n;
    }
  }
  // LinkedIn's DOM (built by JS) can hold lists inside <p>. An HTML parser
  // closes a <p> at the first <ul>, which would push the rest of the
  // description out of its box when Go re-parses it. Serialize <p> as <div>.
  const main = (document.querySelector('main') || document.body).cloneNode(true);
  main.querySelectorAll('p').forEach(p => {
    const d = document.createElement('div');
    for (const a of p.attributes) d.setAttribute(a.name, a.value);
    while (p.firstChild) d.appendChild(p.firstChild);
    p.replaceWith(d);
  });
  return { title: document.title || '', main: main.outerHTML, found: !!box };
})(` + cfg + `)`
}

// Doctor checks the Chrome connection and LinkedIn login by opening /jobs/.
func (c *Client) Doctor(ctx context.Context) (string, error) {
	var msg string
	err := c.browser.with(ctx, func(tab context.Context) error {
		if err := c.browser.navigate(ctx, tab, baseURL+"/jobs/"); err != nil {
			return err
		}
		guest, err := eval[bool](ctx, tab, guestScript)
		if err != nil {
			return err
		}
		if guest {
			msg = "connected to Chrome, but NOT signed in: LinkedIn serves its public pages, which jobscout can read " +
				"(fewer filters, stricter rate limits). Run `jobscout-mcp login` for the full signed-in view."
			return nil
		}
		msg = "connected to Chrome and signed in to LinkedIn"
		return nil
	})
	return msg, err
}

// guestScript is true on LinkedIn's public (signed-out) pages: their header
// always links to sign-in / sign-up.
const guestScript = `!!document.querySelector('header a[href*="/login"], nav a[href*="/login"], header a[href*="/signup"], a[href*="trk=guest_"]')`
