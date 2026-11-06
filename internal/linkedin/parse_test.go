package linkedin

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kushalk47/jobscout/internal/jobs"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseSearchPage(t *testing.T) {
	cards, header, err := ParseSearchPage(read(t, "search.html"), DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, "128 results") {
		t.Errorf("header = %q", header)
	}
	if len(cards) != 2 { // the third card is an un-hydrated shell
		t.Fatalf("got %d cards: %+v", len(cards), cards)
	}
	want := jobs.Card{ID: "4200000001", Title: "Backend Engineer (Go)", Company: "Acme Cloud",
		Location: "Bengaluru, Karnataka, India", Workplace: "Hybrid", Posted: "3 hours ago", EasyApply: true, Promoted: true}
	if cards[0] != want {
		t.Errorf("card 0\n got %+v\nwant %+v", cards[0], want)
	}
	if c := cards[1]; c.Workplace != "Remote" || c.Salary != "₹12L/yr - ₹18L/yr" || c.Posted != "1 day ago" || c.EasyApply {
		t.Errorf("card 1 = %+v", c)
	}
}

func TestParseSearchCardsDrift(t *testing.T) {
	_, err := ParseSearchCards([]string{`<li data-occludable-job-id="4200000009"><div class="renamed">x</div></li>`}, DefaultSelectors())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("want ErrDrift, got %v", err)
	}
}

func TestSelectorOverrideFixesDrift(t *testing.T) {
	snip := `<li data-occludable-job-id="4200000009"><a class="new-title" href="/jobs/view/4200000009/">Go Dev</a></li>`
	sel := DefaultSelectors().Merge(Selectors{SearchTitle: []string{"a.new-title"}})
	cards, err := ParseSearchCards([]string{snip}, sel)
	if err != nil || len(cards) != 1 || cards[0].Title != "Go Dev" {
		t.Fatalf("cards=%+v err=%v", cards, err)
	}
}

func TestParseDetail(t *testing.T) {
	d, err := ParseDetail(read(t, "detail.html"), "4200000001", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Backend Engineer (Go)" || d.Company != "Acme Cloud" {
		t.Errorf("title/company = %q / %q", d.Title, d.Company)
	}
	if d.Location != "Bengaluru, Karnataka, India" || d.Posted != "3 hours ago" || d.Applicants != "Over 100 applicants" {
		t.Errorf("meta = %q | %q | %q", d.Location, d.Posted, d.Applicants)
	}
	if d.Workplace != "Hybrid" || d.Employment != "Full-time" {
		t.Errorf("chips = %q %q", d.Workplace, d.Employment)
	}
	for _, want := range []string{"## What you'll do", "- Build Go microservices on Kubernetes.", "## Requirements", "Strong Go fundamentals.", "- 2+ years of backend experience.", "## Benefits"} {
		if !strings.Contains(d.Description, want) {
			t.Errorf("description missing %q:\n%s", want, d.Description)
		}
	}
	if strings.Contains(d.Description, "Short company blurb") {
		t.Error("picked the About-the-company box instead of About the job")
	}
	if strings.Contains(d.Description, "more") && strings.HasSuffix(d.Description, "more") {
		t.Error("expand button text leaked into description")
	}
}

func TestParseDetailLegacy(t *testing.T) {
	d, err := ParseDetail(read(t, "detail_legacy.html"), "4300000001", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Data Engineer" || d.Company != "Globex" || d.Location != "Pune, Maharashtra, India" || d.Workplace != "On-site" || d.Posted != "2 weeks ago" || d.Applicants != "57 applicants" {
		t.Errorf("got %+v", d)
	}
	if !strings.Contains(d.Description, "- Spark jobs") {
		t.Errorf("description = %q", d.Description)
	}
}

func TestParseDetailDrift(t *testing.T) {
	_, err := ParseDetail(`<html><body><main><p>nothing</p></main></body></html>`, "4300000002", DefaultSelectors())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("want ErrDrift, got %v", err)
	}
}

func TestSearchURL(t *testing.T) {
	u, err := SearchURL(jobs.SearchQuery{Keywords: "golang developer", Location: "Bengaluru", PostedWithinH: 24,
		Workplace: []string{"remote", "hybrid"}, Experience: []string{"entry"}, JobType: []string{"full-time"}, MostRecent: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"keywords=golang+developer", "location=Bengaluru", "f_TPR=r86400", "f_WT=2%2C3", "f_E=2", "f_JT=F", "sortBy=DD", "start=25"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s missing %s", u, want)
		}
	}
	if _, err := SearchURL(jobs.SearchQuery{Workplace: []string{"moon"}}, 0); err == nil {
		t.Error("want error for unknown workplace")
	}
}

func TestGuards(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://www.linkedin.com/jobs/view/123456/": true,
		"https://www.linkedin.com/feed/":             false,
		"http://www.linkedin.com/jobs/":              false,
		"https://evil.com/jobs/":                     false,
	} {
		if AllowedURL(u) != ok {
			t.Errorf("AllowedURL(%s) != %v", u, ok)
		}
	}
	if b, _ := BlockedPage("https://www.linkedin.com/authwall?x=1"); !b {
		t.Error("authwall not detected")
	}
	if JobIDFromHref("/jobs/view/senior-go-engineer-at-acme-4012345678/?x") != "4012345678" {
		t.Error("slug id not parsed")
	}
}

// Real public (signed-out) LinkedIn pages, saved 2026-11-05 and trimmed.
func TestParseGuestSearch(t *testing.T) {
	cards, header, err := ParseSearchPage(read(t, "guest_search.html"), DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, "Golang Developer Jobs in Bengaluru") {
		t.Errorf("header = %q", header)
	}
	if len(cards) != 3 {
		t.Fatalf("got %d cards", len(cards))
	}
	c := cards[0]
	if c.ID != "4429245228" || c.Title != "Senior Software Development Engineer - (GoLang)" || c.Company != "JioStar" ||
		c.Location != "Bengaluru, Karnataka, India" || c.Posted != "2 weeks ago" {
		t.Errorf("card 0 = %+v", c)
	}
	for _, c := range cards {
		if !ValidJobID(c.ID) || c.Title == "" || c.Company == "" {
			t.Errorf("incomplete card %+v", c)
		}
	}
}

func TestParseGuestDetail(t *testing.T) {
	d, err := ParseDetail(read(t, "guest_detail.html"), "4429245228", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Senior Software Development Engineer - (GoLang)" || d.Company != "JioStar" ||
		d.Location != "Bengaluru, Karnataka, India" || d.Posted != "2 weeks ago" || d.Applicants != "Over 200 applicants" {
		t.Errorf("got title=%q company=%q loc=%q posted=%q applicants=%q", d.Title, d.Company, d.Location, d.Posted, d.Applicants)
	}
	if d.Employment != "Full-time" {
		t.Errorf("employment = %q", d.Employment)
	}
	if len(d.Description) < 500 || !strings.Contains(d.Description, "Job Summary") || strings.Contains(d.Description, "Show more") {
		t.Errorf("description (%d chars): %.300s", len(d.Description), d.Description)
	}
	t.Logf("description %d chars, first lines:\n%.400s", len(d.Description), d.Description)
}

// Markup copied from a real guest posting (job 4399367935): headings are <strong>X<br><br></strong>.
func TestStructuredTextBoldWithInnerBreaks(t *testing.T) {
	page := `<html><body><main><div class="show-more-less-html__markup">We need a Go developer.<br><br><strong>Required Skills<br><br></strong>Build scalable APIs<br><br>Write clean code<br><br><strong>Preferred Qualifications<br><br></strong>Minimum 6 months internship<br><br>Use <strong>Go</strong> daily</div></main></body></html>`
	d, err := ParseDetail(page, "4399367935", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	want := "We need a Go developer.\n## Required Skills\nBuild scalable APIs\nWrite clean code\n## Preferred Qualifications\nMinimum 6 months internship\nUse Go daily"
	if d.Description != want {
		t.Errorf("got:\n%s\nwant:\n%s", d.Description, want)
	}
}

// TestTrimOnRealPosting reports how much the trimming saves on a real posting.
func TestTrimOnRealPosting(t *testing.T) {
	d, err := ParseDetail(read(t, "guest_detail.html"), "4429245228", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	tr := jobs.Trim(d.Description, 4000)
	full, trimmed := len(jobs.FormatDetail(d, tr, true)), len(jobs.FormatDetail(d, tr, false))
	t.Logf("real posting: full %d bytes (≈%d tokens) → trimmed %d bytes (≈%d tokens); signals=%q", full, full/4, trimmed, trimmed/4, tr.Signals)
	t.Logf("trimmed text:\n%s", tr.Text)
	if trimmed > full {
		t.Error("trimming grew the output")
	}
}

// Real 2026 signed-in pages (hashed class names), saved 2026-11-06.
func TestParseSignedSearch(t *testing.T) {
	cards, _, err := ParseSearchPage(read(t, "signed_search.html"), DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 6 {
		t.Fatalf("got %d cards: %+v", len(cards), cards)
	}
	want0 := jobs.Card{ID: cards[0].ID, Title: "Golang Developer", Company: "HGS", Location: "Bengaluru", Workplace: "Remote", EasyApply: true}
	if cards[0] != want0 {
		t.Errorf("card 0\n got %+v\nwant %+v", cards[0], want0)
	}
	if c := cards[2]; c.Title != "Software Engineer II" || c.Company != "Couchbase" || c.Posted != "2 weeks ago" {
		t.Errorf("card 2 = %+v", c)
	}
	if c := cards[5]; c.Title != "Golang Developer" || c.Company != "Thales" || c.Workplace != "Hybrid" || c.Posted != "3 days ago" {
		t.Errorf("card 5 (no verified badge) = %+v", c)
	}
}

func TestParseSignedDetail(t *testing.T) {
	d, err := ParseDetail(read(t, "signed_detail.html"), "4414942939", DefaultSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Backend Golang Developer" || d.Company != "Proofpoint" || d.Location != "Bengaluru, Karnataka, India" ||
		d.Posted != "Reposted 1 day ago" || d.Employment != "Full-time" {
		t.Errorf("got title=%q company=%q loc=%q posted=%q emp=%q", d.Title, d.Company, d.Location, d.Posted, d.Employment)
	}
	if !strings.Contains(d.Description, "Develop and maintain microservices using Golang") {
		t.Errorf("description: %.300s", d.Description)
	}
}
