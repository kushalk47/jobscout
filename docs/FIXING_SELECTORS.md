# When LinkedIn changes its page

jobscout reads LinkedIn's visible HTML, so a UI change can break extraction.
The code is laid out so that a fix touches one place.

| Layer | File | Changes when LinkedIn changes? |
|---|---|---|
| MCP tools | `internal/mcpserver/tools.go` | never |
| Use cases, trimming, output | `internal/jobs/` | never |
| Storage | `internal/store/` | never |
| Chrome, pacing, host guard | `internal/linkedin/browser.go` | almost never |
| **Selector table** | `internal/linkedin/selectors.go` | **usual fix** |
| HTML → struct | `internal/linkedin/parse.go` | occasionally |

## Symptoms

The tools return `LinkedIn page layout changed (...)`. jobscout fails loudly
instead of returning an empty list when result cards exist but no field
can be read.

## Quick fix without rebuilding

1. Open the failing page in the jobscout Chrome window, right-click the
   element (job title, company...) → Inspect, and find a stable selector
   (prefer `data-*` attributes, `aria-*` labels and roles over hashed class names).
2. Put it in `~/.jobscout/config.yaml`. Each entry is a list tried in order:

   ```yaml
   selectors:
     search_title: ['a[data-control-name="job_card_title"]', 'a.job-card-list__title--link']
   ```
3. Restart the MCP server (in Claude Code, `/mcp` → reconnect).

## Proper fix

1. Save the page: in the jobscout Chrome window, DevTools → Elements → right-click `<main>` → Copy outerHTML,
   and save it as `internal/linkedin/testdata/<case>.html`. Remove personal data first.
2. Add a test in `internal/linkedin/parse_test.go` that loads it. Watch it fail.
3. Add the new selector at the **front** of the list in `selectors.go`, keeping the old
   ones as fallbacks, or adjust `parse.go` if the structure changed.
4. `go test ./...` and send a PR.

## Checking against the live site

With jobscout's Chrome running (and signed in):

```bash
jobscout-mcp search "golang developer"     # prints the parsed result lines
jobscout-mcp get 4012345678                # prints the parsed, trimmed description
JOBSCOUT_LIVE=1 JOBSCOUT_CDP=http://127.0.0.1:9222 \
  JOBSCOUT_LIVE_DUMP='https://www.linkedin.com/jobs/view/4012345678/' \
  JOBSCOUT_LIVE_OUT=internal/linkedin/testdata/new_case.html \
  go test ./internal/linkedin -run Live -v  # saves <main> of a real page as a new fixture
```
