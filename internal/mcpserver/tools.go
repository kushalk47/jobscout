// Package mcpserver is the thin "controller" layer: four tools, short
// schemas, compact text answers. All logic lives in jobs.Service.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kushalk47/jobscout/internal/jobs"
)

// Tool descriptions are sent to the model on every turn, so they are short.
const (
	descSearch = "Search LinkedIn jobs for several keyword phrases at once. Returns one deduplicated line per job (id|title|company|location|posted|flags), skipping jobs already marked and jobs failing hard filters."
	descGet    = "Read job descriptions by id (max 15 per call). Boilerplate (company pitch, perks, EEO) is removed; requirements and eligibility lines are kept. full=true returns the original text."
	descMark   = "Save your evaluation of jobs locally (never touches LinkedIn): status, score 0-100, short reason. Marked jobs are skipped by later searches."
	descList   = "List marked jobs, best score first. Use for the final shortlist or to review applied jobs."
)

type SearchIn struct {
	Keywords          []string `json:"keywords" jsonschema:"1-8 search phrases"`
	Location          string   `json:"location,omitempty" jsonschema:"city/region/country"`
	PostedWithinDays  int      `json:"posted_within_days,omitempty"`
	Workplace         []string `json:"workplace,omitempty" jsonschema:"remote, hybrid, onsite"`
	Experience        []string `json:"experience,omitempty" jsonschema:"internship, entry, associate, mid_senior, director, executive"`
	JobType           []string `json:"job_type,omitempty" jsonschema:"full_time, part_time, contract, temporary, internship"`
	EasyApplyOnly     bool     `json:"easy_apply_only,omitempty"`
	MostRecent        bool     `json:"most_recent,omitempty" jsonschema:"sort by date instead of relevance"`
	MaxPerKeyword     int      `json:"max_per_keyword,omitempty" jsonschema:"default 25"`
	ExcludeTitleWords []string `json:"exclude_title_words,omitempty" jsonschema:"drop titles containing these words"`
	ExcludeCompanies  []string `json:"exclude_companies,omitempty"`
	IncludeMarked     bool     `json:"include_marked,omitempty"`
}

type GetIn struct {
	IDs  []string `json:"ids" jsonschema:"job ids from jobs_search"`
	Full bool     `json:"full,omitempty"`
}

type MarkItem struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty" jsonschema:"skipped, seen, shortlisted, applied, rejected, not_interested (default seen when scored)"`
	Score  *int   `json:"score,omitempty" jsonschema:"0-100"`
	Reason string `json:"reason,omitempty" jsonschema:"one short line"`
}

type MarkIn struct {
	Marks []MarkItem `json:"marks"`
}

type ListIn struct {
	Status    []string `json:"status,omitempty"`
	MinScore  int      `json:"min_score,omitempty"`
	SinceDays int      `json:"since_days,omitempty"`
	Limit     int      `json:"limit,omitempty" jsonschema:"default 20"`
}

// Defaults applied to searches.
type Defaults struct {
	Location string
	Version  string
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// New builds the MCP server with the four tools.
func New(svc *jobs.Service, d Defaults) *mcp.Server {
	if d.Version == "" {
		d.Version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "jobscout", Version: d.Version}, &mcp.ServerOptions{
		Instructions: "Find LinkedIn jobs with few tokens: jobs_search (screen by line) → jobs_get (only promising ids) → jobs_mark (scores, batch) → jobs_list (shortlist).",
	})

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(true)}
	mcp.AddTool(s, &mcp.Tool{Name: "jobs_search", Description: descSearch, Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, any, error) {
			loc := in.Location
			if loc == "" {
				loc = d.Location
			}
			res, err := svc.Search(ctx, jobs.SearchRequest{
				Keywords: in.Keywords,
				Query: jobs.SearchQuery{
					Location: loc, PostedWithinH: in.PostedWithinDays * 24, Workplace: in.Workplace,
					Experience: in.Experience, JobType: in.JobType, EasyApplyOnly: in.EasyApplyOnly,
					MostRecent: in.MostRecent, MaxResults: in.MaxPerKeyword,
				},
				IncludeMarked: in.IncludeMarked, ExcludeCompanies: in.ExcludeCompanies, ExcludeTitleWords: in.ExcludeTitleWords,
			})
			if err != nil {
				return nil, nil, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "new %d · found %d · dup %d · marked %d · filtered %d\n", len(res.Cards), res.Found, res.Duplicates, res.Marked, res.HardFiltered)
			for _, e := range res.Errors {
				b.WriteString("warn: " + e + "\n")
			}
			if len(res.Cards) > 0 {
				b.WriteString(jobs.FormatCards(res.Cards))
			}
			return text(b.String()), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "jobs_get", Description: descGet, Annotations: readOnly},
		func(ctx context.Context, _ *mcp.CallToolRequest, in GetIn) (*mcp.CallToolResult, any, error) {
			items, err := svc.Get(ctx, in.IDs)
			if err != nil && len(items) == 0 {
				return nil, nil, err
			}
			var b strings.Builder
			for _, it := range items {
				if it.Err != nil {
					fmt.Fprintf(&b, "=== %s | ERROR: %v\n", it.Detail.ID, it.Err)
					continue
				}
				b.WriteString(jobs.FormatDetail(it.Detail, it.Trimmed, in.Full))
			}
			if err != nil {
				fmt.Fprintf(&b, "stopped: %v\n", err)
			}
			return text(b.String()), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "jobs_mark", Description: descMark,
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false), OpenWorldHint: ptr(false)}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in MarkIn) (*mcp.CallToolResult, any, error) {
			marks := make([]jobs.Mark, 0, len(in.Marks))
			for _, m := range in.Marks {
				marks = append(marks, jobs.Mark{ID: m.ID, Status: jobs.Status(strings.ToLower(m.Status)), Score: m.Score, Reason: m.Reason})
			}
			n, err := svc.Mark(ctx, marks)
			if err != nil {
				return nil, nil, err
			}
			return text(fmt.Sprintf("saved %d", n)), nil, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "jobs_list", Description: descList,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in ListIn) (*mcp.CallToolResult, any, error) {
			f := jobs.ListFilter{MinScore: in.MinScore, SinceDays: in.SinceDays, Limit: in.Limit}
			for _, st := range in.Status {
				f.Statuses = append(f.Statuses, jobs.Status(strings.ToLower(st)))
			}
			items, err := svc.List(ctx, f)
			if err != nil {
				return nil, nil, err
			}
			if len(items) == 0 {
				return text("no marked jobs match"), nil, nil
			}
			return text(jobs.FormatList(items)), nil, nil
		})
	return s
}

func ptr[T any](v T) *T { return &v }
