package linkedin

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// abortError marks errors after which further page loads are pointless.
// It satisfies jobs.Aborter so the service stops its keyword loop.
type abortError struct{ msg string }

func (e *abortError) Error() string { return e.msg }
func (e *abortError) Abort() bool   { return true }

// ErrLoginRequired means the Chrome profile is not logged in to LinkedIn.
var ErrLoginRequired error = &abortError{"LinkedIn asked for a sign-in (it limits signed-out browsing after a few pages): run `jobscout-mcp login`, sign in in the Chrome window, then retry"}

// BrowserConfig controls how we reach Chrome.
type BrowserConfig struct {
	CDPURL      string        // e.g. http://127.0.0.1:9222
	ChromePath  string        // Chrome / Chrome for Testing binary; auto-detected when empty
	ProfileDir  string        // dedicated user-data-dir that holds the LinkedIn login
	AutoLaunch  bool          // start Chrome when nothing listens on CDPURL
	Headless    bool          // run the auto-launched Chrome headless
	MinInterval time.Duration // minimum gap between two page loads
	Jitter      time.Duration // extra random gap, 0..Jitter
	NavTimeout  time.Duration
}

// Browser owns ONE tab in a Chrome that runs outside this process.
// All page loads go through it one at a time, with pacing between them.
type Browser struct {
	cfg BrowserConfig

	mu         sync.Mutex // one browser operation at a time
	lastNav    time.Time
	allocStop  context.CancelFunc
	browserCtx context.Context
	tabCtx     context.Context
	tabStop    context.CancelFunc
}

func NewBrowser(cfg BrowserConfig) *Browser {
	if cfg.NavTimeout == 0 {
		cfg.NavTimeout = 45 * time.Second
	}
	return &Browser{cfg: cfg}
}

// Close detaches from Chrome. It closes our tab, never the user's browser.
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resetLocked()
}

func (b *Browser) resetLocked() {
	if b.tabStop != nil {
		b.tabStop()
	}
	if b.allocStop != nil {
		b.allocStop()
	}
	b.tabCtx, b.tabStop, b.browserCtx, b.allocStop = nil, nil, nil, nil
}

// with runs fn on our tab while holding the browser lock.
func (b *Browser) with(ctx context.Context, fn func(tab context.Context) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLocked(ctx); err != nil {
		return err
	}
	err := fn(b.tabCtx)
	if err != nil && b.tabCtx.Err() != nil {
		// Chrome or our tab went away; reconnect on the next call.
		b.resetLocked()
		return fmt.Errorf("lost the Chrome tab (was it closed?): %w", err)
	}
	return err
}

func (b *Browser) ensureLocked(ctx context.Context) error {
	if b.tabCtx != nil && b.tabCtx.Err() == nil {
		return nil
	}
	b.resetLocked()
	if !cdpReachable(ctx, b.cfg.CDPURL) {
		if !b.cfg.AutoLaunch {
			return fmt.Errorf("no Chrome is listening on %s: start Chrome with --remote-debugging-port, or set chrome.auto_launch: true", b.cfg.CDPURL)
		}
		if err := LaunchChrome(b.cfg, ""); err != nil {
			return err
		}
		if err := waitReachable(ctx, b.cfg.CDPURL, 20*time.Second); err != nil {
			return err
		}
	}
	allocCtx, allocStop := remote.NewAllocator(context.Background(), b.cfg.CDPURL)
	browserCtx, _ := chromedp.NewContext(allocCtx)
	// Connect to the browser without opening a tab.
	if _, err := chromedp.Targets(browserCtx); err != nil {
		allocStop()
		return fmt.Errorf("connecting to Chrome at %s: %w", b.cfg.CDPURL, err)
	}
	tabCtx, tabStop := chromedp.NewContext(browserCtx)
	// Open the tab on the long-lived context. chromedp binds the tab's session to
	// the context of its first Run; a short-lived timeout context there would
	// close the session as soon as that first call returned.
	if err := chromedp.Do(tabCtx); err != nil {
		tabStop()
		allocStop()
		return fmt.Errorf("opening a Chrome tab: %w", err)
	}
	b.allocStop, b.browserCtx, b.tabCtx, b.tabStop = allocStop, browserCtx, tabCtx, tabStop
	return nil
}

// pace waits until the minimum gap since the last page load has passed.
func (b *Browser) pace(ctx context.Context) error {
	gap := b.cfg.MinInterval
	if b.cfg.Jitter > 0 {
		gap += time.Duration(rand.Int64N(int64(b.cfg.Jitter)))
	}
	wait := time.Until(b.lastNav.Add(gap))
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// navigate loads target in our tab. Caller holds the lock (inside with).
func (b *Browser) navigate(ctx, tab context.Context, target string) error {
	if !AllowedURL(target) {
		return fmt.Errorf("refusing to open %q: only https://www.linkedin.com/jobs/ pages are allowed", target)
	}
	if err := b.pace(ctx); err != nil {
		return err
	}
	b.lastNav = time.Now()
	navCtx, cancel := context.WithTimeout(tab, b.cfg.NavTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel) // stop the load if the MCP call is cancelled
	defer stop()
	if err := chromedp.Do(navCtx, chromedp.Navigate(target)); err != nil {
		return fmt.Errorf("loading %s: %w", target, err)
	}
	loc, err := chromedp.Run(tab, chromedp.Location())
	if err != nil {
		return err
	}
	if blocked, why := BlockedPage(loc); blocked {
		if why == "not logged in" {
			return ErrLoginRequired
		}
		return &abortError{why + ": open the Chrome window, resolve it by hand, then retry"}
	}
	return nil
}

// eval runs a script in the tab and decodes its JSON result into T.
func eval[T any](ctx, tab context.Context, js string) (T, error) {
	evCtx, cancel := context.WithTimeout(tab, 60*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(evCtx, chromedp.Evaluate[T](js, chromedp.EvalAwaitPromise))
}

// ---- Chrome process helpers -------------------------------------------------

func cdpReachable(ctx context.Context, cdpURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpURL+"/json/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func waitReachable(ctx context.Context, cdpURL string, max time.Duration) error {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cdpReachable(ctx, cdpURL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("Chrome did not open its debugging port %s within %s", cdpURL, max)
}

// CDPReachable is exported for the CLI.
func CDPReachable(ctx context.Context, cdpURL string) bool { return cdpReachable(ctx, cdpURL) }

// LaunchChrome starts Chrome with the dedicated profile and debugging port.
// The process is left running so the login survives MCP server restarts.
func LaunchChrome(cfg BrowserConfig, startURL string) error {
	path := cfg.ChromePath
	if path == "" {
		path = FindChrome()
	}
	if path == "" {
		return errors.New("Chrome not found: set chrome.path in config.yaml (Chrome for Testing or Google Chrome)")
	}
	port, err := portOf(cfg.CDPURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.ProfileDir, 0o700); err != nil {
		return err
	}
	args := []string{
		"--remote-debugging-port=" + port,
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + cfg.ProfileDir,
		"--no-first-run",
		"--no-default-browser-check",
	}
	if cfg.Headless {
		args = append(args, "--headless=new")
	}
	if startURL != "" {
		args = append(args, startURL)
	}
	cmd := exec.Command(path, args...)
	detach(cmd, true)
	if err := cmd.Start(); err != nil {
		// The parent job may forbid breakaway; start Chrome as a plain child.
		cmd = exec.Command(path, args...)
		detach(cmd, false)
		if err2 := cmd.Start(); err2 != nil {
			return fmt.Errorf("starting Chrome %s: %w", path, err2)
		}
	}
	return cmd.Process.Release()
}

// OpenLogin shows the LinkedIn sign-in page in the dedicated Chrome profile,
// starting that Chrome when needed. It is the only non-/jobs/ page we open,
// and only on the user's explicit `jobscout-mcp login` command.
func OpenLogin(ctx context.Context, cfg BrowserConfig) error {
	const loginURL = "https://www.linkedin.com/login"
	if !cdpReachable(ctx, cfg.CDPURL) {
		visible := cfg
		visible.Headless = false
		return LaunchChrome(visible, loginURL)
	}
	allocCtx, allocStop := remote.NewAllocator(context.Background(), cfg.CDPURL)
	defer allocStop()
	browserCtx, _ := chromedp.NewContext(allocCtx)
	tab, _ := chromedp.NewContext(browserCtx, chromedp.WithDetachOnCancel())
	return chromedp.Do(tab, chromedp.Navigate(loginURL))
}

func portOf(cdpURL string) (string, error) {
	u, err := url.Parse(cdpURL)
	if err != nil {
		return "", fmt.Errorf("bad chrome.cdp_url %q: %w", cdpURL, err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", fmt.Errorf("chrome.cdp_url %q needs an explicit port: %w", cdpURL, err)
	}
	return port, nil
}

// FindChrome looks for Chrome for Testing first, then regular Chrome/Chromium.
func FindChrome() string {
	if p := os.Getenv("JOBSCOUT_CHROME"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		candidates = append(candidates, filepath.Join(home, ".jobscout", "chrome-for-testing", "chrome-win64", "chrome.exe"))
		candidates = append(candidates, globNewest(filepath.Join(home, ".cache", "puppeteer", "chrome", "*", "chrome-win64", "chrome.exe"))...)
		candidates = append(candidates, globNewest(filepath.Join(local, "ms-playwright", "chromium-*", "chrome-win64", "chrome.exe"))...)
		candidates = append(candidates,
			filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(pf86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(local, "Google", "Chrome", "Application", "chrome.exe"),
		)
	case "darwin":
		candidates = append(candidates,
			filepath.Join(home, ".jobscout", "chrome-for-testing", "chrome-mac-arm64", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"),
			filepath.Join(home, ".jobscout", "chrome-for-testing", "chrome-mac-x64", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"),
			"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
	default:
		candidates = append(candidates, filepath.Join(home, ".jobscout", "chrome-for-testing", "chrome-linux64", "chrome"))
		candidates = append(candidates, globNewest(filepath.Join(home, ".cache", "puppeteer", "chrome", "*", "chrome-linux64", "chrome"))...)
		candidates = append(candidates, globNewest(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome"))...)
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if p, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, p)
			}
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func globNewest(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	// Reverse lexical order puts the newest version directory first.
	for i, j := 0, len(m)-1; i < j; i, j = i+1, j-1 {
		m[i], m[j] = m[j], m[i]
	}
	return m
}
