package jobs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kushalk47/jobscout/internal/jobs"
	"github.com/kushalk47/jobscout/internal/store"
)

const sampleDesc = `Acme Cloud is a fast-growing company building the future of developer platforms for teams worldwide. We have offices in 12 countries and have raised a large Series C from top investors, and our customers include many of the Fortune 500 who rely on us every single day to ship software faster and more safely than ever before.
## What you'll do
- Build Go microservices on Kubernetes.
- Own PostgreSQL schemas and performance.
## Requirements
Strong Go fundamentals.
- 2+ years of backend experience.
- B.Tech in Computer Science or equivalent.
## Benefits
- Free lunch.
- Health insurance.
- Hybrid work from our Bengaluru office 3 days a week.
## Equal Opportunity
Acme is an equal opportunity employer regardless of race, religion or gender.
Candidates must be authorized to work in India; we do not offer visa sponsorship.`

func TestTrimKeepsEligibilityDropsBoilerplate(t *testing.T) {
	tr := jobs.Trim(sampleDesc, 0)
	for _, want := range []string{"## What you'll do", "## Requirements", "2+ years of backend experience", "B.Tech", "Hybrid work from our Bengaluru office", "do not offer visa sponsorship"} {
		if !strings.Contains(tr.Text, want) {
			t.Errorf("trimmed text lost %q:\n%s", want, tr.Text)
		}
	}
	for _, gone := range []string{"Free lunch", "Health insurance", "equal opportunity employer", "Fortune 500"} {
		if strings.Contains(tr.Text, gone) {
			t.Errorf("trimmed text kept %q", gone)
		}
	}
	if !tr.Trimmed || len(tr.Text) >= tr.OriginalChars {
		t.Errorf("not trimmed: %d -> %d", tr.OriginalChars, len(tr.Text))
	}
	if tr.Signals != "exp 2+y; degree mentioned; NO visa sponsorship" {
		t.Errorf("signals = %q", tr.Signals)
	}
}

func TestTrimKeepsAboutTheJob(t *testing.T) {
	tr := jobs.Trim("## About the job\nYou will build APIs.\n## About Acme\nWe are great.", 0)
	if !strings.Contains(tr.Text, "You will build APIs.") || strings.Contains(tr.Text, "We are great") {
		t.Errorf("got %q", tr.Text)
	}
}

func TestTrimNoHeadingsReturnsWhole(t *testing.T) {
	in := "We need a Go developer.\nYou will write services.\nThree years required."
	tr := jobs.Trim(in, 0)
	if tr.Trimmed || tr.Text != in {
		t.Errorf("got trimmed=%v %q", tr.Trimmed, tr.Text)
	}
}

func TestTrimCapIsRuneSafe(t *testing.T) {
	tr := jobs.Trim(strings.Repeat("₹ salary band ", 100), 50)
	if !strings.Contains(tr.Text, "full=true") || !strings.HasPrefix(tr.Text, "₹") {
		t.Errorf("got %q", tr.Text)
	}
}

func TestShortFormats(t *testing.T) {
	for in, want := range map[string]string{"3 hours ago": "3h", "Reposted 2 weeks ago": "2w", "1 month ago": "1mo", "an hour ago": "1h", "": ""} {
		if got := jobs.ShortAgo(in); got != want {
			t.Errorf("ShortAgo(%q)=%q want %q", in, got, want)
		}
	}
	if got := jobs.ShortLocation("Bengaluru, Karnataka, India", "Hybrid"); got != "Bengaluru, India (Hybrid)" {
		t.Errorf("ShortLocation = %q", got)
	}
	out := jobs.FormatCards([]jobs.Card{{ID: "4200000001", Title: "Go | Backend", Company: "Acme", Location: "India", Workplace: "Remote", Posted: "1 day ago", EasyApply: true}})
	if out != "id|title|company|location|posted|flags\n4200000001|Go / Backend|Acme|India (Remote)|1d|easy\n" {
		t.Errorf("FormatCards = %q", out)
	}
}

// ---- service with a fake source and a real in-memory SQLite store ----------

type fakeSource struct {
	byKeyword map[string][]jobs.Card
	details   map[string]jobs.Detail
	gets      int
	err       error
}

func (f *fakeSource) Search(_ context.Context, q jobs.SearchQuery) ([]jobs.Card, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byKeyword[q.Keywords], nil
}

func (f *fakeSource) Get(_ context.Context, id string) (jobs.Detail, error) {
	f.gets++
	d, ok := f.details[id]
	if !ok {
		return jobs.Detail{}, errors.New("not found")
	}
	return d, nil
}

type abortErr struct{}

func (abortErr) Error() string { return "not logged in" }
func (abortErr) Abort() bool   { return true }

func newService(t *testing.T, src jobs.Source, opt jobs.Options) (*jobs.Service, *store.SQLite) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return jobs.NewService(src, st, opt), st
}

func card(id, title, company string) jobs.Card {
	return jobs.Card{ID: id, Title: title, Company: company, Location: "India"}
}

func TestServiceFlow(t *testing.T) {
	ctx := context.Background()
	src := &fakeSource{
		byKeyword: map[string][]jobs.Card{
			"golang developer": {card("4200000001", "Golang Developer", "Acme"), card("4200000002", "Senior Go Engineer", "Orbit"), card("4200000003", "Go Dev", "BadCorp India")},
			"backend sde 1":    {card("4200000001", "Golang Developer", "Acme"), card("4200000004", "SDE 1 Backend", "Zeta")},
		},
		details: map[string]jobs.Detail{"4200000001": {ID: "4200000001", Title: "Golang Developer", Company: "Acme", Description: sampleDesc}},
	}
	svc, _ := newService(t, src, jobs.Options{ExcludeCompanies: []string{"badcorp"}})

	res, err := svc.Search(ctx, jobs.SearchRequest{Keywords: []string{"golang developer", "backend sde 1", "Golang Developer"}, ExcludeTitleWords: []string{"senior"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Found != 5 || res.Duplicates != 1 || res.HardFiltered != 2 || len(res.Cards) != 2 {
		t.Fatalf("result = %+v", res)
	}

	// Score one, skip one: both disappear from the next search.
	score := 87
	if _, err := svc.Mark(ctx, []jobs.Mark{{ID: "4200000001", Score: &score, Reason: "Go + 2y match"}, {ID: "4200000004", Status: jobs.StatusSkipped}}); err != nil {
		t.Fatal(err)
	}
	res, _ = svc.Search(ctx, jobs.SearchRequest{Keywords: []string{"golang developer", "backend sde 1"}, ExcludeTitleWords: []string{"senior"}})
	if len(res.Cards) != 0 || res.Marked != 2 {
		t.Fatalf("after marks = %+v", res)
	}

	// Detail is fetched once, then served from cache.
	for i := 0; i < 2; i++ {
		items, err := svc.Get(ctx, []string{"4200000001"})
		if err != nil || items[0].Err != nil {
			t.Fatal(err, items[0].Err)
		}
		if !strings.Contains(items[0].Trimmed.Text, "2+ years") {
			t.Errorf("trimmed = %q", items[0].Trimmed.Text)
		}
	}
	if src.gets != 1 {
		t.Errorf("source hit %d times, want 1", src.gets)
	}

	// Status change keeps the score.
	if _, err := svc.Mark(ctx, []jobs.Mark{{ID: "4200000001", Status: jobs.StatusApplied}}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx, jobs.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Card.ID != "4200000001" || *list[0].Score != 87 || list[0].Status != jobs.StatusApplied || list[0].Title != "Golang Developer" {
		t.Fatalf("list = %+v", list)
	}
	out := jobs.FormatList(list[:1])
	if !strings.Contains(out, "87|4200000001|Golang Developer|Acme|India|applied|Go + 2y match") {
		t.Errorf("FormatList = %q", out)
	}
}

func TestServiceStopsOnAbort(t *testing.T) {
	svc, _ := newService(t, &fakeSource{err: abortErr{}}, jobs.Options{})
	_, err := svc.Search(context.Background(), jobs.SearchRequest{Keywords: []string{"a", "b"}})
	if err == nil || err.Error() != "not logged in" {
		t.Fatalf("err = %v", err)
	}
}

func TestMarkValidation(t *testing.T) {
	svc, _ := newService(t, &fakeSource{}, jobs.Options{})
	bad := 140
	for _, m := range []jobs.Mark{{ID: "abc", Status: jobs.StatusSeen}, {ID: "4200000001"}, {ID: "4200000001", Status: "loved"}, {ID: "4200000001", Score: &bad}} {
		if _, err := svc.Mark(context.Background(), []jobs.Mark{m}); err == nil {
			t.Errorf("mark %+v accepted", m)
		}
	}
}

func TestGetBatchLimit(t *testing.T) {
	svc, _ := newService(t, &fakeSource{}, jobs.Options{MaxIDsPerGet: 2})
	if _, err := svc.Get(context.Background(), []string{"1000001", "1000002", "1000003"}); err == nil {
		t.Error("batch limit not enforced")
	}
}

func TestSignalsMonths(t *testing.T) {
	if s := jobs.Trim("Minimum 6 months internship. 1-3 years preferred. 6 plus years for lead. 4 or more years.", 0).Signals; s != "exp 6mo,1-3y,6+y" {
		t.Errorf("signals = %q", s)
	}
}

func TestTrimPlainHeadings(t *testing.T) {
	in := "## How We Work\nWe are BRAVE.\nCorporate Overview\nWe protect the Fortune 1000.\n## Job Description\nBuild Go microservices.\nKey Responsibilities\nDebug distributed systems.\nBenefits\nFree snacks."
	tr := jobs.Trim(in, 0)
	for _, gone := range []string{"BRAVE", "Fortune 1000", "Free snacks"} {
		if strings.Contains(tr.Text, gone) {
			t.Errorf("kept %q:\n%s", gone, tr.Text)
		}
	}
	for _, kept := range []string{"Build Go microservices.", "## Key Responsibilities", "Debug distributed systems."} {
		if !strings.Contains(tr.Text, kept) {
			t.Errorf("lost %q:\n%s", kept, tr.Text)
		}
	}
}
