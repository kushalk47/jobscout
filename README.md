# wellfound-mcp

A small MCP server for finding and applying to jobs on Wellfound from Claude.
It has five tools, returns compact JSON, and needs your approval before every submission.

## Setup (CachyOS / Arch)

```bash
sudo pacman -S uv
cd ~/DEV/wellfound-mcp
uv sync
uv run playwright install chromium
uv run wellfound-mcp-login            # log in by hand once, press Enter
claude mcp add wellfound -- uv run --directory ~/DEV/wellfound-mcp wellfound-mcp
```

Don't use `playwright install --with-deps`, because it needs `apt`. If Chromium
can't find a library, install it with pacman.

## Tools

| Tool | What it does |
|---|---|
| `check_login` | Checks that the saved session is still logged in |
| `search_jobs(role, location?, remote?, limit=10)` | Returns compact rows and marks jobs you've already handled |
| `get_job(job_id)` | Returns requirements, stack, comp and a trimmed description. Takes the full id from search, like `4417481-software-engineer` |
| `draft_application(job_id)` | Lists the form's questions. Nothing is submitted |
| `submit_application(job_id, answers, confirm=false)` | Returns a preview, and only submits with `confirm=true` |
| `list_applications(status?)` | Lists your local log of drafted and submitted jobs |

## Settings (env vars)

| Variable | Default | Purpose |
|---|---|---|
| `WELLFOUND_MCP_DATA` | `~/.local/share/wellfound-mcp` | Browser profile and SQLite log |
| `WELLFOUND_HEADLESS` | `0` | `1` hides the browser |
| `WELLFOUND_MIN_DELAY` / `MAX_DELAY` | `3` / `7` | Random pause between page loads (seconds) |
| `WELLFOUND_DAILY_LIMIT` | `15` | Max submissions per day |

The browser profile holds your logged-in session, so treat it like a password.

## Status

Working against the live site: `check_login`, `search_jobs`, `get_job`, plus the
rate limiting, local log and approval gate. Search reads the Next.js Apollo cache
on `/role/...` pages, and job details come from the schema.org JSON-LD on
`/jobs/<id>-<slug>` (an expired job returns 410).

Not built yet: `draft_application` and `submit_application`. Seeing the apply form
needs a logged-in session. The Apply button opens `?autoOpenApplication=true`.

Run the parser tests with `uv run pytest`.
