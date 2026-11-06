# wellfound-mcp

A small MCP server for finding and applying to jobs on Wellfound from Claude.
It has a handful of small tools, returns compact JSON, and needs your approval before every submission.

## Setup (CachyOS / Arch)

```bash
sudo pacman -S uv
cd ~/DEV/wellfound-mcp
uv sync
uv run playwright install chromium
cp profile.example.md profile.md      # then fill in profile.md
uv run wellfound-mcp-login            # log in by hand once, press Enter
claude mcp add wellfound -- uv run --directory ~/DEV/wellfound-mcp wellfound-mcp
```

Don't use `playwright install --with-deps`, because it needs `apt`. If Chromium
can't find a library, install it with pacman.

## Tools

| Tool | What it does |
|---|---|
| `check_login` | Checks that the saved session is still logged in |
| `get_profile` | Reads `profile.md` (copy it from `profile.example.md`) |
| `save_answer(question, answer)` | Adds an answer you gave to `profile.md`, so it isn't asked again |
| `search_jobs(role, location?, remote?, limit=10)` | Returns compact rows, hides skipped jobs and marks ones you've drafted or applied to |
| `get_job(job_id)` | Returns requirements, stack, comp and a trimmed description. Takes the full id from search, like `4417481-software-engineer` |
| `skip_job(job_ids, reason?, confirm=false, undo=false)` | Previews, then skips jobs (or un-skips them) once you approve |
| `draft_application(job_id)` | Lists the form's questions. Nothing is submitted |
| `submit_application(job_id, answers, confirm=false)` | Returns a preview, and only submits with `confirm=true` |
| `list_applications(status?)` | Lists your local log of drafted and submitted jobs |

## Settings (env vars)

| Variable | Default | Purpose |
|---|---|---|
| `WELLFOUND_MCP_DATA` | `./data` (project folder) | Browser profile and SQLite log |
| `WELLFOUND_HEADLESS` | `0` | `1` hides the browser |
| `WELLFOUND_MIN_DELAY` / `MAX_DELAY` | `3` / `7` | Random pause between page loads (seconds) |
| `WELLFOUND_DAILY_LIMIT` | `15` | Max submissions per day |
| `WELLFOUND_PROFILE` | `./profile.md` (project root) | Your background and standard answers |

Everything stays inside the project folder. `data/` holds your logged-in browser
session (treat it like a password) and the application log, and `profile.md` holds
your personal details. `.gitignore` keeps both out of git; only the blank
`profile.example.md` is committed.

## How answers work

Claude reads `profile.md` once per session and answers form questions from it.
If a question isn't covered, Claude asks you rather than guessing, then offers to
save your answer with `save_answer`, so each new question only comes up once.

## Status

Working against the live site: `check_login`, `search_jobs`, `get_job`, plus the
rate limiting, local log and approval gate. Search reads the Next.js Apollo cache
on `/role/...` pages, and job details come from the schema.org JSON-LD on
`/jobs/<id>-<slug>` (an expired job returns 410).

Not built yet: `draft_application` and `submit_application`. Seeing the apply form
needs a logged-in session. The Apply button opens `?autoOpenApplication=true`.

Run the parser tests with `uv run pytest`.
