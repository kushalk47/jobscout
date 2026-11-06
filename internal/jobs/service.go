package jobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Options are the server-side defaults (from config.yaml).
type Options struct {
	ExcludeCompanies  []string      // case-insensitive substring match
	ExcludeTitleWords []string      // whole-word, case-insensitive ("senior", "lead", "manager")
	SkipPromoted      bool          // drop "Promoted" cards
	DetailMaxAge      time.Duration // reuse cached descriptions younger than this
	MaxDescChars      int           // cap on a trimmed description
	MaxIDsPerGet      int           // keep one jobs_get call bounded in time
	MaxKeywords       int
}

// Service holds the use cases. It is the only thing the MCP layer talks to.
type Service struct {
	src   Source
	store Store
	opt   Options
	now   func() time.Time
}

func NewService(src Source, store Store, opt Options) *Service {
	if opt.MaxIDsPerGet <= 0 {
		opt.MaxIDsPerGet = 15
	}
	if opt.MaxKeywords <= 0 {
		opt.MaxKeywords = 8
	}
	if opt.DetailMaxAge <= 0 {
		opt.DetailMaxAge = 7 * 24 * time.Hour
	}
	return &Service{src: src, store: store, opt: opt, now: time.Now}
}

// Aborter is implemented by source errors that make further calls pointless
// (not logged in, security checkpoint). The search loop stops on them.
type Aborter interface{ Abort() bool }

func isAbort(err error) bool {
	var a Aborter
	return errors.As(err, &a) && a.Abort() || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// SearchRequest is jobs_search's input after validation.
type SearchRequest struct {
	Keywords          []string
	Query             SearchQuery // filters shared by every keyword; Keywords field ignored
	IncludeMarked     bool
	ExcludeCompanies  []string
	ExcludeTitleWords []string
}

// SearchResult carries the cards Claude should screen plus counters.
type SearchResult struct {
	Cards                                   []Card
	Found, Duplicates, Marked, HardFiltered int
	Errors                                  []string
}

// Search runs every keyword, merges and dedupes by job id, stores the cards,
// and removes jobs already marked and jobs that fail the hard filters.
func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	var res SearchResult
	kws := dedupeStrings(req.Keywords)
	if len(kws) == 0 {
		return res, errors.New("give at least one keyword")
	}
	if len(kws) > s.opt.MaxKeywords {
		return res, fmt.Errorf("at most %d keywords per call (got %d)", s.opt.MaxKeywords, len(kws))
	}
	marked, err := s.store.MarkedIDs(ctx)
	if err != nil {
		return res, err
	}
	filter := newHardFilter(append(s.opt.ExcludeCompanies, req.ExcludeCompanies...), append(s.opt.ExcludeTitleWords, req.ExcludeTitleWords...), s.opt.SkipPromoted)

	byID := map[string]bool{}
	var all []Card
	okCount := 0
	for _, kw := range kws {
		q := req.Query
		q.Keywords = kw
		cards, err := s.src.Search(ctx, q)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%q: %v", kw, err))
			if isAbort(err) {
				return res, err
			}
			continue
		}
		okCount++
		for _, c := range cards {
			res.Found++
			if byID[c.ID] {
				res.Duplicates++
				continue
			}
			byID[c.ID] = true
			all = append(all, c)
		}
	}
	if okCount == 0 {
		return res, fmt.Errorf("every search failed: %s", strings.Join(res.Errors, "; "))
	}
	if err := s.store.UpsertCards(ctx, all); err != nil {
		return res, err
	}
	for _, c := range all {
		if _, ok := marked[c.ID]; ok && !req.IncludeMarked {
			res.Marked++
			continue
		}
		if !filter.keep(c) {
			res.HardFiltered++
			continue
		}
		res.Cards = append(res.Cards, c)
	}
	return res, nil
}

// GetItem is one jobs_get result.
type GetItem struct {
	Detail  Detail
	Trimmed Trimmed
	Cached  bool
	Err     error
}

// Get returns descriptions, from the local cache when fresh.
func (s *Service) Get(ctx context.Context, ids []string) ([]GetItem, error) {
	ids = dedupeStrings(ids)
	if len(ids) == 0 {
		return nil, errors.New("give at least one job id")
	}
	if len(ids) > s.opt.MaxIDsPerGet {
		return nil, fmt.Errorf("at most %d ids per call (got %d): split into batches", s.opt.MaxIDsPerGet, len(ids))
	}
	out := make([]GetItem, 0, len(ids))
	for _, id := range ids {
		it := GetItem{}
		cached, err := s.store.GetDetail(ctx, id, s.opt.DetailMaxAge)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			it.Detail, it.Cached = *cached, true
		} else {
			d, err := s.src.Get(ctx, id)
			if err != nil {
				it.Detail.ID, it.Err = id, err
				out = append(out, it)
				if isAbort(err) {
					return out, err
				}
				continue
			}
			d.FetchedAt = s.now()
			if err := s.store.SaveDetail(ctx, d); err != nil {
				return nil, err
			}
			it.Detail = d
		}
		it.Trimmed = Trim(it.Detail.Description, s.opt.MaxDescChars)
		out = append(out, it)
	}
	return out, nil
}

// Mark validates and stores evaluations in one batch.
func (s *Service) Mark(ctx context.Context, marks []Mark) (int, error) {
	if len(marks) == 0 {
		return 0, errors.New("give at least one mark")
	}
	now := s.now()
	for i := range marks {
		m := &marks[i]
		m.ID = strings.TrimSpace(m.ID)
		if !jobIDRe.MatchString(m.ID) {
			return 0, fmt.Errorf("mark %d: invalid job id %q", i, m.ID)
		}
		if m.Status == "" {
			if m.Score != nil {
				m.Status = StatusSeen
			} else {
				return 0, fmt.Errorf("mark %s: status is required", m.ID)
			}
		}
		if !ValidStatus(m.Status) {
			return 0, fmt.Errorf("mark %s: unknown status %q", m.ID, m.Status)
		}
		if m.Score != nil && (*m.Score < 0 || *m.Score > 100) {
			return 0, fmt.Errorf("mark %s: score must be 0-100", m.ID)
		}
		if len(m.Reason) > 300 {
			m.Reason = m.Reason[:300]
		}
		m.UpdatedAt = now
	}
	return len(marks), s.store.SaveMarks(ctx, marks)
}

// List returns marked jobs, best score first.
func (s *Service) List(ctx context.Context, f ListFilter) ([]ListedJob, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 20
	}
	for _, st := range f.Statuses {
		if !ValidStatus(st) {
			return nil, fmt.Errorf("unknown status %q", st)
		}
	}
	return s.store.List(ctx, f)
}

// ---- hard filters ----------------------------------------------------------

type hardFilter struct {
	companies    []string
	titleWords   *regexp.Regexp
	skipPromoted bool
}

func newHardFilter(companies, titleWords []string, skipPromoted bool) hardFilter {
	f := hardFilter{skipPromoted: skipPromoted}
	for _, c := range companies {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			f.companies = append(f.companies, c)
		}
	}
	var words []string
	for _, w := range titleWords {
		if w = strings.TrimSpace(w); w != "" {
			words = append(words, regexp.QuoteMeta(w))
		}
	}
	if len(words) > 0 {
		f.titleWords = regexp.MustCompile(`(?i)\b(?:` + strings.Join(words, "|") + `)\b`)
	}
	return f
}

func (f hardFilter) keep(c Card) bool {
	if f.skipPromoted && c.Promoted {
		return false
	}
	if f.titleWords != nil && f.titleWords.MatchString(c.Title) {
		return false
	}
	company := strings.ToLower(c.Company)
	for _, x := range f.companies {
		if strings.Contains(company, x) {
			return false
		}
	}
	return true
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		k := strings.ToLower(s)
		if s == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

var jobIDRe = regexp.MustCompile(`^[0-9]{5,30}$`)
