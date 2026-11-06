package linkedin

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kushalk47/jobscout/internal/jobs"
)

const (
	baseURL     = "https://www.linkedin.com"
	pageSize    = 25 // LinkedIn shows 25 results per search page
	allowedHost = "www.linkedin.com"
)

// LinkedIn's URL filter codes (same codes the official site uses in its query string).
var (
	workplaceCodes  = map[string]string{"onsite": "1", "remote": "2", "hybrid": "3"}
	experienceCodes = map[string]string{"internship": "1", "entry": "2", "associate": "3", "mid_senior": "4", "director": "5", "executive": "6"}
	jobTypeCodes    = map[string]string{"full_time": "F", "part_time": "P", "contract": "C", "temporary": "T", "internship": "I", "volunteer": "V", "other": "O"}
)

var jobIDPattern = regexp.MustCompile(`^[0-9]{5,30}$`)
var jobIDInURL = regexp.MustCompile(`/jobs/view/(?:[^/?#]*-)?([0-9]{5,30})`)

// ValidJobID reports whether id looks like a LinkedIn job id.
func ValidJobID(id string) bool { return jobIDPattern.MatchString(id) }

// JobURL is the canonical job URL.
func JobURL(id string) string { return fmt.Sprintf("%s/jobs/view/%s/", baseURL, id) }

// JobIDFromHref extracts the numeric id from a /jobs/view/... link.
func JobIDFromHref(href string) string {
	m := jobIDInURL.FindStringSubmatch(href)
	if m == nil {
		return ""
	}
	return m[1]
}

// SearchURL builds the search URL for one keyword query and page.
func SearchURL(q jobs.SearchQuery, page int) (string, error) {
	v := url.Values{}
	if q.Keywords != "" {
		v.Set("keywords", q.Keywords)
	}
	if q.Location != "" {
		v.Set("location", q.Location)
	}
	if q.PostedWithinH > 0 {
		v.Set("f_TPR", fmt.Sprintf("r%d", q.PostedWithinH*3600))
	}
	add := func(param string, vals []string, codes map[string]string, kind string) error {
		if len(vals) == 0 {
			return nil
		}
		out := make([]string, 0, len(vals))
		for _, raw := range vals {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "-", "_"))
			code, ok := codes[key]
			if !ok {
				return fmt.Errorf("unknown %s %q", kind, raw)
			}
			out = append(out, code)
		}
		v.Set(param, strings.Join(out, ","))
		return nil
	}
	if err := add("f_WT", q.Workplace, workplaceCodes, "workplace"); err != nil {
		return "", err
	}
	if err := add("f_E", q.Experience, experienceCodes, "experience level"); err != nil {
		return "", err
	}
	if err := add("f_JT", q.JobType, jobTypeCodes, "job type"); err != nil {
		return "", err
	}
	if q.EasyApplyOnly {
		v.Set("f_AL", "true")
	}
	if q.MostRecent {
		v.Set("sortBy", "DD")
	}
	if page > 0 {
		v.Set("start", fmt.Sprint(page*pageSize))
	}
	return baseURL + "/jobs/search/?" + v.Encode(), nil
}

// AllowedURL is the host guard: the browser may only be sent to LinkedIn job pages.
func AllowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != allowedHost {
		return false
	}
	return strings.HasPrefix(u.Path, "/jobs/")
}

// BlockedPage reports whether LinkedIn redirected to a login wall or checkpoint.
func BlockedPage(finalURL string) (blocked bool, reason string) {
	u, err := url.Parse(finalURL)
	if err != nil {
		return false, ""
	}
	p := u.Path
	switch {
	case strings.HasPrefix(p, "/login"), strings.HasPrefix(p, "/uas/login"), strings.HasPrefix(p, "/authwall"), strings.HasPrefix(p, "/signup"):
		return true, "not logged in"
	case strings.HasPrefix(p, "/checkpoint"):
		return true, "LinkedIn security checkpoint"
	}
	return false, ""
}
