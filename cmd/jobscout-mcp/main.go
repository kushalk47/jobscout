// Command jobscout-mcp is a lightweight, token-frugal MCP server for finding
// LinkedIn jobs through your own logged-in Chrome.
//
//	jobscout-mcp            run the MCP server on stdio (what MCP clients launch)
//	jobscout-mcp login      open the dedicated Chrome profile on LinkedIn's sign-in page
//	jobscout-mcp doctor     check Chrome, login and the selector table
//	jobscout-mcp init       write ~/.jobscout/config.yaml with every option documented
//	jobscout-mcp version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kushalk47/jobscout/internal/config"
	"github.com/kushalk47/jobscout/internal/jobs"
	"github.com/kushalk47/jobscout/internal/linkedin"
	"github.com/kushalk47/jobscout/internal/mcpserver"
	"github.com/kushalk47/jobscout/internal/store"
)

var version = "dev" // set by goreleaser

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr) // stdout belongs to the MCP protocol

	fs := flag.NewFlagSet("jobscout-mcp", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file (default ~/.jobscout/config.yaml)")
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch cmd {
	case "serve":
		err = serve(ctx, cfg)
	case "login":
		err = login(ctx, cfg)
	case "doctor":
		err = doctor(ctx, cfg)
	case "init":
		err = initConfig(cfg)
	case "search": // debug: jobscout-mcp search "golang developer" [location]
		err = debugSearch(ctx, cfg, fs.Args())
	case "get": // debug: jobscout-mcp get <job id>
		err = debugGet(ctx, cfg, fs.Args())
	case "bench": // jobscout-mcp bench "kw1" "kw2" ... : measures token cost of a real run
		err = bench(ctx, cfg, fs.Args())
	case "clear-cache": // drop cached descriptions after a parser fix; marks are kept
		err = clearCache(ctx, cfg)
	case "tool": // jobscout-mcp tool jobs_search '{"keywords":["go developer"]}'
		err = callTool(ctx, cfg, fs.Args())
	case "version":
		fmt.Println("jobscout-mcp", version)
	default:
		err = fmt.Errorf("unknown command %q (serve, login, doctor, init, version)", cmd)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func build(cfg config.Config) (*jobs.Service, *linkedin.Client, func(), error) {
	if err := os.MkdirAll(cfg.Home, 0o700); err != nil {
		return nil, nil, nil, err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, nil, err
	}
	browser := linkedin.NewBrowser(cfg.BrowserConfig())
	sel := linkedin.DefaultSelectors().Merge(cfg.Selectors)
	client := linkedin.NewClient(browser, sel, cfg.Search.MaxPagesPerKeyword)
	svc := jobs.NewService(client, st, jobs.Options{
		ExcludeCompanies:  cfg.Search.ExcludeCompanies,
		ExcludeTitleWords: cfg.Search.ExcludeTitleWords,
		SkipPromoted:      cfg.Search.SkipPromoted,
		DetailMaxAge:      time.Duration(cfg.Details.CacheDays) * 24 * time.Hour,
		MaxDescChars:      cfg.Details.MaxChars,
		MaxIDsPerGet:      cfg.Details.MaxIDsPerCall,
		MaxKeywords:       cfg.Search.MaxKeywords,
	})
	closeAll := func() { browser.Close(); st.Close() }
	return svc, client, closeAll, nil
}

func serve(ctx context.Context, cfg config.Config) error {
	svc, _, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	srv := mcpserver.New(svc, mcpserver.Defaults{Location: cfg.Search.DefaultLocation, Version: version})
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func login(ctx context.Context, cfg config.Config) error {
	if err := linkedin.OpenLogin(ctx, cfg.BrowserConfig()); err != nil {
		return err
	}
	fmt.Println("Chrome is open on LinkedIn's sign-in page (profile: " + cfg.Chrome.ProfileDir + ").")
	fmt.Println("Sign in there, keep that Chrome running, then run `jobscout-mcp doctor`.")
	return nil
}

func doctor(ctx context.Context, cfg config.Config) error {
	fmt.Println("home:     ", cfg.Home)
	fmt.Println("cdp_url:  ", cfg.Chrome.CDPURL)
	path := cfg.Chrome.Path
	if path == "" {
		path = linkedin.FindChrome()
	}
	fmt.Println("chrome:   ", orNone(path))
	fmt.Println("reachable:", linkedin.CDPReachable(ctx, cfg.Chrome.CDPURL))
	_, client, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	msg, err := client.Doctor(ctx)
	if err != nil {
		return err
	}
	fmt.Println("linkedin: ", msg)
	return nil
}

func initConfig(cfg config.Config) error {
	path := filepath.Join(cfg.Home, "config.yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(cfg.Home, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(config.Example), 0o600); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

// debugSearch prints exactly what jobs_search would return, without marking anything.
func debugSearch(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return errors.New(`usage: jobscout-mcp search "keywords" [location]`)
	}
	q := jobs.SearchQuery{Keywords: args[0], Location: cfg.Search.DefaultLocation, MaxResults: 25}
	if len(args) > 1 {
		q.Location = args[1]
	}
	_, client, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	cards, err := client.Search(ctx, q)
	if err != nil {
		return err
	}
	fmt.Printf("%d results\n%s", len(cards), jobs.FormatCards(cards))
	return nil
}

// debugGet prints exactly what jobs_get would return for one id.
func debugGet(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: jobscout-mcp get <job id>")
	}
	_, client, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	d, err := client.Get(ctx, args[0])
	if err != nil {
		return err
	}
	t := jobs.Trim(d.Description, cfg.Details.MaxChars)
	fmt.Print(jobs.FormatDetail(d, t, false))
	return nil
}

// bench runs the real search → get path and reports approximate tokens
// (bytes/4) of what the model would read, trimmed versus untrimmed.
func bench(ctx context.Context, cfg config.Config, kws []string) error {
	if len(kws) == 0 {
		return errors.New(`usage: jobscout-mcp bench "keyword one" "keyword two" ...`)
	}
	svc, _, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	res, err := svc.Search(ctx, jobs.SearchRequest{Keywords: kws, Query: jobs.SearchQuery{Location: cfg.Search.DefaultLocation}, IncludeMarked: true})
	if err != nil {
		return err
	}
	searchOut := jobs.FormatCards(res.Cards)
	fmt.Printf("search: %d keywords → %d found, %d duplicates removed, %d unique\n", len(kws), res.Found, res.Duplicates, len(res.Cards))
	fmt.Printf("search output: %d bytes ≈ %d tokens (≈ %d per job)\n", len(searchOut), len(searchOut)/4, len(searchOut)/4/max(1, len(res.Cards)))
	n := min(10, len(res.Cards))
	ids := make([]string, 0, n)
	for _, c := range res.Cards[:n] {
		ids = append(ids, c.ID)
	}
	items, err := svc.Get(ctx, ids)
	if err != nil && len(items) == 0 {
		return err
	}
	var full, trimmed, ok int
	for _, it := range items {
		if it.Err != nil {
			fmt.Printf("  %s: %v\n", it.Detail.ID, it.Err)
			continue
		}
		ok++
		f := len(jobs.FormatDetail(it.Detail, it.Trimmed, true))
		t := len(jobs.FormatDetail(it.Detail, it.Trimmed, false))
		full += f
		trimmed += t
		fmt.Printf("  %s: %5d → %5d bytes  %s\n", it.Detail.ID, f, t, it.Trimmed.Signals)
	}
	if ok > 0 {
		fmt.Printf("descriptions: %d jobs, full ≈ %d tokens, trimmed ≈ %d tokens (%.0f%% saved)\n", ok, full/4, trimmed/4, 100*(1-float64(trimmed)/float64(full)))
	}
	return nil
}

// callTool runs one MCP tool through an in-memory MCP client and prints its
// text answer: the exact bytes an MCP client would receive.
func callTool(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New(`usage: jobscout-mcp tool <jobs_search|jobs_get|jobs_mark|jobs_list> '<json arguments>'`)
	}
	var arguments map[string]any
	if len(args) == 2 {
		if err := json.Unmarshal([]byte(args[1]), &arguments); err != nil {
			return fmt.Errorf("arguments: %w", err)
		}
	}
	svc, _, closeAll, err := build(cfg)
	if err != nil {
		return err
	}
	defer closeAll()
	srv := mcpserver.New(svc, mcpserver.Defaults{Location: cfg.Search.DefaultLocation, Version: version})
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		return err
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "jobscout-cli"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return err
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: args[0], Arguments: arguments})
	if err != nil {
		return err
	}
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			fmt.Print(t.Text)
		}
	}
	if res.IsError {
		return errors.New("tool returned an error")
	}
	return nil
}

func clearCache(ctx context.Context, cfg config.Config) error {
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.ClearDetails(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("cleared %d cached descriptions (marks kept)\n", n)
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(not found — set chrome.path)"
	}
	return s
}
