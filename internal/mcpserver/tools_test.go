package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kushalk47/jobscout/internal/jobs"
	"github.com/kushalk47/jobscout/internal/mcpserver"
	"github.com/kushalk47/jobscout/internal/store"
)

type fake struct{}

func (fake) Search(_ context.Context, q jobs.SearchQuery) ([]jobs.Card, error) {
	return []jobs.Card{
		{ID: "4200000001", Title: "Golang Developer", Company: "Acme", Location: "Bengaluru, Karnataka, India", Workplace: "Hybrid", Posted: "2 days ago", EasyApply: true},
		{ID: "4200000002", Title: "Backend SDE 1", Company: "Zeta", Location: "India", Workplace: "Remote", Posted: "5 hours ago"},
	}, nil
}

func (fake) Get(_ context.Context, id string) (jobs.Detail, error) {
	return jobs.Detail{ID: id, Title: "Golang Developer", Company: "Acme", Location: "Bengaluru, Karnataka, India",
		Description: "## Requirements\n- 1-3 years of Go experience.\n## Perks\n- Snacks"}, nil
}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := mcpserver.New(jobs.NewService(fake{}, st, jobs.Options{}), mcpserver.Defaults{Location: "India"})
	ct, sTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, sTransport, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestToolsOverMCP(t *testing.T) {
	cs := connect(t)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 4 {
		t.Fatalf("got %d tools", len(tools.Tools))
	}
	raw, _ := json.Marshal(tools.Tools)
	t.Logf("tools/list payload: %d bytes ≈ %d tokens", len(raw), len(raw)/4)
	if len(raw) > 6000 {
		t.Errorf("tool definitions grew to %d bytes; keep them lean", len(raw))
	}
	for _, tool := range tools.Tools {
		if tool.OutputSchema != nil {
			t.Errorf("%s has an output schema (wastes tokens)", tool.Name)
		}
	}

	out, isErr := call(t, cs, "jobs_search", map[string]any{"keywords": []string{"golang developer", "sde 1 backend"}})
	if isErr {
		t.Fatal(out)
	}
	want := "new 2 · found 4 · dup 2 · marked 0 · filtered 0\nid|title|company|location|posted|flags\n" +
		"4200000001|Golang Developer|Acme|Bengaluru, India (Hybrid)|2d|easy\n" +
		"4200000002|Backend SDE 1|Zeta|India (Remote)|5h|\n"
	if out != want {
		t.Errorf("jobs_search:\n%s\nwant:\n%s", out, want)
	}

	out, _ = call(t, cs, "jobs_get", map[string]any{"ids": []string{"4200000001"}})
	if !strings.Contains(out, "1-3 years of Go") || strings.Contains(out, "Snacks") || !strings.Contains(out, "signals: exp 1-3y") {
		t.Errorf("jobs_get:\n%s", out)
	}

	out, isErr = call(t, cs, "jobs_mark", map[string]any{"marks": []map[string]any{
		{"id": "4200000001", "score": 82, "reason": "Go 1-3y fits"},
		{"id": "4200000002", "status": "skipped"},
	}})
	if isErr || out != "saved 2" {
		t.Fatalf("jobs_mark: %s", out)
	}

	out, _ = call(t, cs, "jobs_search", map[string]any{"keywords": []string{"golang developer"}})
	if !strings.HasPrefix(out, "new 0 ·") {
		t.Errorf("marked jobs not skipped: %s", out)
	}

	out, _ = call(t, cs, "jobs_list", map[string]any{"min_score": 50})
	if !strings.Contains(out, "82|4200000001|Golang Developer|Acme|Bengaluru, India (Hybrid)|seen|Go 1-3y fits") || strings.Contains(out, "4200000002") {
		t.Errorf("jobs_list:\n%s", out)
	}

	out, isErr = call(t, cs, "jobs_mark", map[string]any{"marks": []map[string]any{{"id": "4200000001", "status": "loved"}}})
	if !isErr || !strings.Contains(out, "unknown status") {
		t.Errorf("bad status accepted: %s", out)
	}
}
