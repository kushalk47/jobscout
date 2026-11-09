package linkedin

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/kushalk47/jobscout/internal/jobs"
)

// Parsing is pure: HTML in, domain structs out. No browser, no network, so
// every function here is unit-tested against saved pages in testdata/.

// ErrDrift means LinkedIn's markup no longer matches the selector table.
var ErrDrift = errors.New("LinkedIn page layout changed (selectors in internal/linkedin/selectors.go or config.yaml need an update)")

var (
	postedPattern     = regexp.MustCompile(`(?i)\b(?:reposted\s+)?(?:\d+\s+)?(?:minute|hour|day|week|month|year)s?\s+ago\b|\bjust now\b`)
	applicantsPattern = regexp.MustCompile(`(?i)\b(?:(?:over|under)\s+)?[\d,.+]+\s+(?:applicants?|people clicked apply)\b`)
	salaryPattern     = regexp.MustCompile(`[$€£₹¥]|\b(?:USD|INR|EUR|GBP|CAD|LPA)\b|/(?:yr|year|hr|hour|month|mo)\b`)
	workplaceSuffix   = regexp.MustCompile(`(?i)^(.+?)\s*\((remote|hybrid|on-site)\)$`)
	titleCountPrefix  = regexp.MustCompile(`^\(\d+\)\s*`)
	spaceRun          = regexp.MustCompile(`\s+`)
	moreSuffix        = regexp.MustCompile(`(?i)\s*(?:…|\.\.\.)\s*(?:see\s+)?more\s*$`)
)

var workplaceWords = map[string]string{"remote": "Remote", "hybrid": "Hybrid", "on-site": "On-site", "onsite": "On-site"}
var employmentWords = map[string]string{"full-time": "Full-time", "part-time": "Part-time", "contract": "Contract", "temporary": "Temporary", "internship": "Internship", "volunteer": "Volunteer"}

func collapse(s string) string { return strings.TrimSpace(spaceRun.ReplaceAllString(s, " ")) }

// firstText returns the collapsed text of the first selector that matches with non-empty text.
func firstText(root *goquery.Selection, sels []string) string {
	for _, s := range sels {
		var out string
		root.Find(s).EachWithBreak(func(_ int, el *goquery.Selection) bool {
			out = collapse(textOf(el.Nodes[0]))
			return out == ""
		})
		if out != "" {
			return out
		}
	}
	return ""
}

// ParseSearchPage parses a full saved search page (used by tests and as a fallback).
func ParseSearchPage(page string, sel Selectors) (cards []jobs.Card, header string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return nil, "", err
	}
	header = firstText(doc.Selection, sel.SearchHeader)
	var snippets []string
	for _, s := range sel.SearchCard {
		doc.Find(s).Each(func(_ int, el *goquery.Selection) {
			if h, err := goquery.OuterHtml(el); err == nil {
				snippets = append(snippets, h)
			}
		})
		if len(snippets) > 0 {
			break
		}
	}
	cards, err = ParseSearchCards(snippets, sel)
	return cards, header, err
}

// ParseSearchCards parses the outer HTML of each result card.
// Cards without a title (not yet hydrated by LinkedIn) are skipped; if every
// card fails, the layout has drifted and ErrDrift is returned.
func ParseSearchCards(snippets []string, sel Selectors) ([]jobs.Card, error) {
	out := make([]jobs.Card, 0, len(snippets))
	seen := map[string]bool{}
	for _, snip := range snippets {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(snip))
		if err != nil {
			continue
		}
		root := doc.Find("body").Children().First()
		c, ok := parseCard(root, sel)
		if !ok || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	if len(snippets) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("%w: %d result cards found but none had an id and title", ErrDrift, len(snippets))
	}
	return out, nil
}

func parseCard(root *goquery.Selection, sel Selectors) (jobs.Card, bool) {
	var c jobs.Card
	for _, attr := range sel.SearchCardIDAttr {
		if v, ok := root.Attr(attr); ok && idFromAttr(v) != "" {
			c.ID = idFromAttr(v)
			break
		}
		if v, ok := root.Find("[" + attr + "]").First().Attr(attr); ok && idFromAttr(v) != "" {
			c.ID = idFromAttr(v)
			break
		}
	}
	if c.ID == "" {
		for _, s := range sel.SearchTitleLink {
			if href, ok := root.Find(s).First().Attr("href"); ok {
				if id := JobIDFromHref(href); id != "" {
					c.ID = id
					break
				}
			}
		}
	}
	c.Title = titleSuffix.ReplaceAllString(firstText(root, sel.SearchTitle), "")
	if c.ID == "" || c.Title == "" {
		return c, false
	}
	c.Company = firstText(root, sel.SearchCompany)
	location := firstText(root, sel.SearchLocation)

	var footer []string
	for _, s := range sel.SearchFooter {
		root.Find(s).Each(func(_ int, el *goquery.Selection) {
			if t := collapse(el.Text()); t != "" {
				footer = append(footer, t)
			}
		})
		if len(footer) > 0 {
			break
		}
	}

	// Layouts with hashed class names (2026 signed-in UI) are read by line
	// order: title, company, location, then badges ("Posted 2 days ago", "Easy Apply").
	if c.Company == "" && location == "" && len(footer) == 0 {
		var lines []string
		for _, s := range sel.SearchLines {
			root.Find(s).Each(func(_ int, el *goquery.Selection) {
				t := titleSuffix.ReplaceAllString(collapse(textOf(el.Nodes[0])), "")
				if t != "" && !strings.Contains(t, c.Title) && strings.Trim(t, "·• ") != "" {
					lines = append(lines, t)
				}
			})
			if len(lines) > 0 {
				break
			}
		}
		if len(lines) > 0 {
			c.Company = lines[0]
		}
		if len(lines) > 1 {
			location = lines[1]
		}
		if len(lines) > 2 {
			footer = lines[2:]
		}
	}
	c.Location, c.Workplace = splitWorkplace(location)
	for _, f := range footer {
		low := strings.ToLower(f)
		switch {
		case c.Posted == "" && postedPattern.MatchString(f):
			c.Posted = postedPattern.FindString(f)
		case low == "easy apply":
			c.EasyApply = true
		case strings.HasPrefix(low, "promoted"):
			c.Promoted = true
		case c.Salary == "" && salaryPattern.MatchString(f):
			c.Salary = f
		}
	}
	return c, true
}

var (
	urnID       = regexp.MustCompile(`^(?:urn:li:(?:jobPosting|fs_normalized_jobPosting):|job-card-component-ref-)([0-9]{5,30})$`)
	titleSuffix = regexp.MustCompile(`(?i)\s*(?:with verification|\(verified job\))$`)
)

// idFromAttr accepts "4012345678", "urn:li:jobPosting:4012345678" or
// "job-card-component-ref-4012345678".
func idFromAttr(v string) string {
	v = strings.TrimSpace(v)
	if ValidJobID(v) {
		return v
	}
	if m := urnID.FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return ""
}

func splitWorkplace(loc string) (string, string) {
	if m := workplaceSuffix.FindStringSubmatch(loc); m != nil {
		return strings.TrimSpace(m[1]), workplaceWords[strings.ToLower(m[2])]
	}
	return loc, ""
}

// ParseDetail parses a job detail page.
func ParseDetail(page, id string, sel Selectors) (jobs.Detail, error) {
	d := jobs.Detail{ID: id}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return d, err
	}
	main := doc.Find("main").First()
	if main.Length() == 0 {
		main = doc.Selection
	}

	desc := pickDescription(main, sel)
	if desc == nil {
		return d, fmt.Errorf("%w: no job description element on job %s", ErrDrift, id)
	}
	d.Description = moreSuffix.ReplaceAllString(StructuredText(desc), "")
	if strings.TrimSpace(d.Description) == "" {
		return d, fmt.Errorf("%w: empty job description on job %s", ErrDrift, id)
	}

	// <title> is "Title | Company | LinkedIn" and is the most stable fallback.
	var titleParts []string
	if t := collapse(doc.Find("title").First().Text()); t != "" {
		titleParts = strings.Split(titleCountPrefix.ReplaceAllString(t, ""), " | ")
	}
	d.Title = firstText(main, sel.DetailTitle)
	if d.Title == "" && len(titleParts) >= 2 {
		d.Title = strings.TrimSpace(titleParts[0])
	}
	d.Company = firstText(main, sel.DetailCompany)
	if d.Company == "" && len(titleParts) >= 3 {
		d.Company = strings.TrimSpace(titleParts[1])
	}

	top := firstSelection(main, sel.DetailTopCard)
	topText := ""
	if top != nil {
		topText = collapse(textOf(top.Nodes[0]))
	}
	// Find the "Location · 3 days ago · 120 applicants" line: the first block
	// in page order that holds it (the top card, not "similar jobs" further
	// down), narrowed to its innermost element so company/title wrappers lose.
	isMeta := func(t string) bool {
		return len(t) < 300 && strings.Contains(t, "·") && postedPattern.MatchString(t)
	}
	scan := main
	if top != nil {
		scan = top
	}
	var metaEl *goquery.Selection
	scan.Find("p, span, div").EachWithBreak(func(_ int, el *goquery.Selection) bool {
		if isMeta(collapse(textOf(el.Nodes[0]))) {
			metaEl = el
			return false
		}
		return true
	})
	metaLine := ""
	for metaEl != nil {
		metaLine = collapse(textOf(metaEl.Nodes[0]))
		var inner *goquery.Selection
		metaEl.Find("p, span, div").EachWithBreak(func(_ int, el *goquery.Selection) bool {
			if isMeta(collapse(textOf(el.Nodes[0]))) {
				inner = el
				return false
			}
			return true
		})
		metaEl = inner
	}
	if metaLine != "" {
		parts := strings.Split(metaLine, "·")
		d.Location = strings.TrimSpace(parts[0])
		d.Posted = postedPattern.FindString(metaLine)
		d.Applicants = applicantsPattern.FindString(metaLine)
	} else if topText != "" {
		d.Posted = postedPattern.FindString(topText)
		d.Applicants = applicantsPattern.FindString(topText)
	}
	// Layouts with dedicated elements (guest pages) override the "·" heuristics.
	if v := firstText(main, sel.DetailLocation); v != "" {
		d.Location = v
	}
	if v := firstText(main, sel.DetailPosted); v != "" {
		d.Posted = v
	}
	if v := firstText(main, sel.DetailApplicants); v != "" {
		d.Applicants = applicantsPattern.FindString(v)
	}
	d.Location, d.Workplace = splitWorkplace(d.Location)
	d.Apply = applyTarget(main, sel)

	// Workplace / employment "chips" are short exact labels near the top.
	// Guest pages list them under "Employment type" below the description.
	scanChips := func(root *goquery.Selection) {
		root.Find("a, span, button, li, strong").EachWithBreak(func(i int, el *goquery.Selection) bool {
			if desc.HasNodes(el.Nodes[0]).Length() > 0 { // never read "remote" out of the description text
				return true
			}
			t := strings.ToLower(collapse(el.Text()))
			if w, ok := workplaceWords[t]; ok && d.Workplace == "" {
				d.Workplace = w
			}
			if e, ok := employmentWords[t]; ok && d.Employment == "" {
				d.Employment = e
			}
			return i < 2000 && (d.Workplace == "" || d.Employment == "")
		})
	}
	if top != nil {
		scanChips(top)
	}
	if d.Employment == "" || d.Workplace == "" {
		scanChips(main)
	}
	return d, nil
}

func firstSelection(root *goquery.Selection, sels []string) *goquery.Selection {
	for _, s := range sels {
		if f := root.Find(s).First(); f.Length() > 0 {
			return f
		}
	}
	return nil
}

// pickDescription prefers the box under an "About the job" heading, else the longest match.
func pickDescription(main *goquery.Selection, sel Selectors) *goquery.Selection {
	for _, s := range sel.DetailDescription {
		matches := main.Find(s)
		if matches.Length() == 0 {
			continue
		}
		var best *goquery.Selection
		bestLen := -1
		matches.Each(func(_ int, el *goquery.Selection) {
			if best != nil && bestLen == 1<<30 {
				return
			}
			if underAboutHeading(el, sel.DetailAboutHeader) {
				best, bestLen = el, 1<<30
				return
			}
			if n := len(collapse(el.Text())); n > bestLen {
				best, bestLen = el, n
			}
		})
		if best != nil && bestLen > 0 {
			return best
		}
	}
	return nil
}

// underAboutHeading walks up to the nearest ancestor that has a heading outside
// el itself, and reports whether that nearest heading is "About the job".
func underAboutHeading(el *goquery.Selection, headers []string) bool {
	anc := el
	for i := 0; i < 5; i++ {
		anc = anc.Parent()
		if anc.Length() == 0 {
			return false
		}
		hs := anc.Find("h1, h2, h3, h4").FilterFunction(func(_ int, h *goquery.Selection) bool {
			return el.HasNodes(h.Nodes[0]).Length() == 0 && h.Nodes[0] != el.Nodes[0]
		})
		if hs.Length() == 0 {
			continue
		}
		matched := false
		hs.Each(func(_ int, h *goquery.Selection) {
			t := strings.ToLower(collapse(h.Text()))
			for _, want := range headers {
				if t == strings.ToLower(want) {
					matched = true
				}
			}
		})
		return matched
	}
	return false
}

// ---- HTML → structured text ------------------------------------------------

var skipTags = map[string]bool{"script": true, "style": true, "svg": true, "button": true, "noscript": true, "template": true}
var blockTags = map[string]bool{"p": true, "div": true, "section": true, "article": true, "ul": true, "ol": true, "tr": true, "table": true, "header": true, "footer": true, "blockquote": true}
var headingTags = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}

// textOf returns the visible text of n with spaces at element boundaries.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if skipTags[n.Data] || hiddenNode(n) {
				return
			}
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			b.WriteByte(' ')
		}
	}
	walk(n)
	return b.String()
}

// hiddenNode skips screen-reader duplicates like <span class="visually-hidden">.
func hiddenNode(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "class" && (strings.Contains(a.Val, "visually-hidden") || strings.Contains(a.Val, "a11y-text")) {
			return true
		}
	}
	return false
}

// StructuredText converts a description element to lines:
// headings become "## Heading", list items "- item", paragraphs plain lines.
func StructuredText(sel *goquery.Selection) string {
	w := &textWriter{}
	for _, n := range sel.Nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.walk(c)
		}
	}
	w.flush()
	return strings.Join(w.lines, "\n")
}

type textWriter struct {
	lines []string
	cur   strings.Builder
}

func (w *textWriter) flush() {
	if t := collapse(w.cur.String()); t != "" {
		w.lines = append(w.lines, t)
	}
	w.cur.Reset()
}

func (w *textWriter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.cur.WriteString(n.Data)
		return
	case html.ElementNode:
	default:
		return
	}
	tag := n.Data
	switch {
	case skipTags[tag] || hiddenNode(n):
		return
	case tag == "br":
		w.flush()
		return
	case headingTags[tag]:
		w.flush()
		if t := collapse(textOf(n)); t != "" {
			w.lines = append(w.lines, "## "+strings.TrimSuffix(t, ":"))
		}
		return
	case tag == "li":
		w.flush()
		if t := collapse(textOf(n)); t != "" {
			w.lines = append(w.lines, "- "+t)
		}
		return
	case blockTags[tag]:
		w.flush()
		if t := collapse(textOf(n)); t != "" && len(t) <= 80 && boldOnly(n) {
			w.lines = append(w.lines, "## "+strings.TrimSuffix(t, ":"))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.walk(c)
		}
		w.flush()
		return
	}
	// Inline element: a bold run on its own line acts as a heading. LinkedIn
	// posts write it as <strong>X</strong><br> or <strong>X<br><br></strong>.
	if (tag == "strong" || tag == "b") && collapse(w.cur.String()) == "" && (isBr(n.NextSibling) || isBr(lastElement(n)) || n.NextSibling == nil) {
		if t := collapse(textOf(n)); t != "" && len(t) <= 80 {
			w.lines = append(w.lines, "## "+strings.TrimSuffix(t, ":"))
			return
		}
	}
	w.cur.WriteByte(' ')
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
	w.cur.WriteByte(' ')
}

// boldOnly reports whether all visible text of n sits inside <strong>/<b>.
func boldOnly(n *html.Node) bool {
	var total, bold strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inBold bool) {
		if n.Type == html.TextNode {
			total.WriteString(n.Data)
			if inBold {
				bold.WriteString(n.Data)
			}
			return
		}
		if n.Type == html.ElementNode && (skipTags[n.Data] || hiddenNode(n)) {
			return
		}
		b := inBold || (n.Type == html.ElementNode && (n.Data == "strong" || n.Data == "b"))
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, b)
		}
	}
	walk(n, false)
	t := collapse(total.String())
	return t != "" && t == collapse(bold.String())
}

func isBr(n *html.Node) bool { return n != nil && n.Type == html.ElementNode && n.Data == "br" }

func lastElement(n *html.Node) *html.Node {
	for c := n.LastChild; c != nil; c = c.PrevSibling {
		if c.Type == html.ElementNode {
			return c
		}
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			return nil
		}
	}
	return nil
}

// applyTarget returns "easy" for LinkedIn Easy Apply, else the company's own
// apply URL (unwrapped from LinkedIn's /safety/go/?url= redirect), else "".
func applyTarget(main *goquery.Selection, sel Selectors) string {
	for _, s := range sel.DetailApply {
		var found string
		main.Find(s).EachWithBreak(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			found = unwrapApplyURL(href)
			return found == ""
		})
		if found != "" {
			return found
		}
	}
	if firstSelection(main, sel.DetailEasyApply) != nil {
		return "easy"
	}
	return ""
}

func unwrapApplyURL(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Host == "" {
		return ""
	}
	if strings.HasSuffix(u.Host, "linkedin.com") {
		inner := u.Query().Get("url")
		if inner == "" {
			return ""
		}
		return unwrapApplyURL(inner)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return u.String()
}
