package linkedin

import (
	"context"
	"github.com/kushalk47/jobscout/internal/jobs"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// TestLiveChrome checks the real Chrome connection step by step.
// Opt-in: JOBSCOUT_LIVE=1 JOBSCOUT_CDP=http://127.0.0.1:9222 go test ./internal/linkedin -run Live -v
func TestLiveChrome(t *testing.T) {
	if os.Getenv("JOBSCOUT_LIVE") != "1" {
		t.Skip("set JOBSCOUT_LIVE=1 to run against a real Chrome")
	}
	cdp := os.Getenv("JOBSCOUT_CDP")
	if cdp == "" {
		cdp = "http://127.0.0.1:9222"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if !cdpReachable(ctx, cdp) {
		t.Fatalf("nothing on %s", cdp)
	}
	allocCtx, allocStop := remote.NewAllocator(context.Background(), cdp)
	defer allocStop()
	browserCtx, _ := chromedp.NewContext(allocCtx)
	start := time.Now()
	if _, err := chromedp.Targets(browserCtx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Logf("attached in %s", time.Since(start))
	tab, tabStop := chromedp.NewContext(browserCtx)
	defer tabStop()
	start = time.Now()
	navCtx, navCancel := context.WithTimeout(tab, 45*time.Second)
	defer navCancel()
	if err := chromedp.Do(navCtx, chromedp.Navigate("https://www.linkedin.com/jobs/")); err != nil {
		t.Fatalf("navigate: %v (after %s)", err, time.Since(start))
	}
	t.Logf("navigated in %s", time.Since(start))
	loc, err := chromedp.Run(tab, chromedp.Location())
	t.Logf("location=%s err=%v", loc, err)
	for name, opts := range map[string][]chromedp.EvaluateOption{"plain": nil, "await": {chromedp.EvalAwaitPromise}} {
		evCtx, evCancel := context.WithTimeout(tab, 10*time.Second)
		start = time.Now()
		title, err := chromedp.Run(evCtx, chromedp.Evaluate[string](`document.title`, opts...))
		evCancel()
		t.Logf("eval %s: %q err=%v in %s", name, title, err, time.Since(start))
	}
	{
		start = time.Now()
		got, err := eval[string](ctx, tab, `(async () => { await new Promise(r => setTimeout(r, 100)); return document.title })()`)
		t.Logf("eval helper async: %q err=%v in %s", got, err, time.Since(start))
	}
	// JOBSCOUT_LIVE_DUMP=<linkedin jobs url> JOBSCOUT_LIVE_OUT=<file> saves <main> for new testdata.
	if u, out := os.Getenv("JOBSCOUT_LIVE_DUMP"), os.Getenv("JOBSCOUT_LIVE_OUT"); u != "" && out != "" {
		if err := chromedp.Do(tab, chromedp.Navigate(u)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		page, err := eval[string](ctx, tab, `'<html><head><title>' + document.title.replace(/</g,'&lt;') + '</title></head><body>' + (document.querySelector('main') || document.body).outerHTML + '</body></html>'`)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved %d bytes to %s", len(page), out)
	}
	if q := os.Getenv("JOBSCOUT_LIVE_QUERY"); q != "" {
		b := NewBrowser(BrowserConfig{CDPURL: cdp, MinInterval: time.Second})
		defer b.Close()
		c := NewClient(b, DefaultSelectors(), 1)
		cards, err := c.Search(ctx, searchQuery(q))
		t.Logf("search %q: %d cards err=%v", q, len(cards), err)
		for i, card := range cards {
			if i < 5 {
				t.Logf("  %+v", card)
			}
		}
	}
}

func searchQuery(q string) jobs.SearchQuery { return jobs.SearchQuery{Keywords: q, MaxResults: 25} }
