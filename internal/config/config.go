// Package config loads ~/.jobscout/config.yaml. Every field has a default,
// so the file is optional.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kushalk47/jobscout/internal/linkedin"
)

type Config struct {
	Home string `yaml:"-"`

	Chrome struct {
		CDPURL     string `yaml:"cdp_url"`
		Path       string `yaml:"path"`
		ProfileDir string `yaml:"profile_dir"`
		AutoLaunch bool   `yaml:"auto_launch"`
		Headless   bool   `yaml:"headless"`
	} `yaml:"chrome"`

	Pacing struct {
		MinInterval time.Duration `yaml:"min_interval"`
		Jitter      time.Duration `yaml:"jitter"`
	} `yaml:"pacing"`

	Search struct {
		MaxPagesPerKeyword int      `yaml:"max_pages_per_keyword"`
		MaxKeywords        int      `yaml:"max_keywords"`
		DefaultLocation    string   `yaml:"default_location"`
		ExcludeCompanies   []string `yaml:"exclude_companies"`
		ExcludeTitleWords  []string `yaml:"exclude_title_words"`
		SkipPromoted       bool     `yaml:"skip_promoted"`
	} `yaml:"search"`

	Details struct {
		CacheDays     int `yaml:"cache_days"`
		MaxChars      int `yaml:"max_chars"`
		MaxIDsPerCall int `yaml:"max_ids_per_call"`
	} `yaml:"details"`

	// Selectors overrides entries of linkedin.DefaultSelectors() — the no-rebuild fix for UI changes.
	Selectors linkedin.Selectors `yaml:"selectors"`
}

// HomeDir is $JOBSCOUT_HOME or ~/.jobscout.
func HomeDir() (string, error) {
	if h := os.Getenv("JOBSCOUT_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jobscout"), nil
}

func defaults(home string) Config {
	var c Config
	c.Home = home
	c.Chrome.CDPURL = "http://127.0.0.1:9222"
	c.Chrome.ProfileDir = filepath.Join(home, "chrome-profile")
	c.Chrome.AutoLaunch = true
	c.Pacing.MinInterval = 4 * time.Second
	c.Pacing.Jitter = 2 * time.Second
	c.Search.MaxPagesPerKeyword = 2
	c.Search.MaxKeywords = 8
	c.Details.CacheDays = 7
	c.Details.MaxChars = 4000
	c.Details.MaxIDsPerCall = 15
	return c
}

// Load reads path (or <home>/config.yaml when path is empty). A missing file is fine.
func Load(path string) (Config, error) {
	home, err := HomeDir()
	if err != nil {
		return Config{}, err
	}
	c := defaults(home)
	if path == "" {
		path = filepath.Join(home, "config.yaml")
	}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("%s: %w", path, err)
		}
	}
	c.Chrome.ProfileDir = expandHome(c.Chrome.ProfileDir)
	c.Chrome.Path = expandHome(c.Chrome.Path)
	c.Chrome.CDPURL = strings.TrimRight(c.Chrome.CDPURL, "/")
	if c.Pacing.MinInterval < time.Second {
		c.Pacing.MinInterval = time.Second // never hammer LinkedIn
	}
	if c.Search.MaxPagesPerKeyword < 1 || c.Search.MaxPagesPerKeyword > 4 {
		c.Search.MaxPagesPerKeyword = 2
	}
	return c, nil
}

func (c Config) DBPath() string { return filepath.Join(c.Home, "jobscout.db") }

func (c Config) BrowserConfig() linkedin.BrowserConfig {
	return linkedin.BrowserConfig{
		CDPURL: c.Chrome.CDPURL, ChromePath: c.Chrome.Path, ProfileDir: c.Chrome.ProfileDir,
		AutoLaunch: c.Chrome.AutoLaunch, Headless: c.Chrome.Headless,
		MinInterval: c.Pacing.MinInterval, Jitter: c.Pacing.Jitter,
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// Example is written by `jobscout-mcp init`.
const Example = `# jobscout config — every key is optional.
chrome:
  cdp_url: http://127.0.0.1:9222   # Chrome DevTools port jobscout attaches to
  path: ""                         # Chrome for Testing / Chrome binary; auto-detected when empty
  profile_dir: ~/.jobscout/chrome-profile   # dedicated profile that holds your LinkedIn login
  auto_launch: true                # start Chrome when nothing listens on cdp_url
  headless: false

pacing:                            # gap between page loads; keep it human
  min_interval: 4s
  jitter: 2s

search:
  max_pages_per_keyword: 2         # 25 results per page
  max_keywords: 8
  default_location: ""             # e.g. "Bengaluru, Karnataka, India"
  exclude_companies: []            # e.g. ["Some Staffing Co"]
  exclude_title_words: []          # e.g. ["senior", "lead", "principal", "manager"]
  skip_promoted: false

details:
  cache_days: 7                    # reuse fetched descriptions
  max_chars: 4000                  # cap per trimmed description
  max_ids_per_call: 15

# If LinkedIn changes its page, override selectors here without rebuilding.
# Keys: search_card, search_card_id_attr, search_title, search_title_link, search_company,
# search_location, search_footer, search_header, search_no_results, detail_description,
# detail_expand, detail_title, detail_company, detail_top_card, detail_about_header
selectors: {}
`
