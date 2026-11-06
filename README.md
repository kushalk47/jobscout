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
uv run wellfound-mcp-login            # log in inside the window it opens; it closes itself
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
| `WELLFOUND_DAILY_START` | `5` | Daily cap to start at. It rises by 1 for each day you apply without Wellfound blocking you, and drops back here if it does |
| `WELLFOUND_DAILY_LIMIT` | `15` | The most the daily cap can rise to |
| `WELLFOUND_PROFILE` | `./profile.md` (project root) | Your background and standard answers |

Wellfound also has its own cap on how many applications can be **active at once**
(the number isn't published). Slots free up as older applications expire. If you hit
it, `draft_application` and `submit_application` return `limit_reached: true` along
with Wellfound's message. Because slots are limited, apply selectively.

Everything stays inside the project folder. `data/` holds your logged-in browser
session (treat it like a password) and the application log, and `profile.md` holds
your personal details. `.gitignore` keeps both out of git; only the blank
`profile.example.md` is committed.

## Pacing and retries

Every page load waits a random 3–7s, then scrolls and moves the mouse a little.
Answers are typed with uneven keystroke timing, and the mouse moves to a button
before clicking it. Page loads that fail (network errors, 429, 5xx) are retried up
to 3 times with backoff. If Cloudflare shows a check, the server waits up to 2
minutes for you to clear it in the browser window. "Send application" is never
retried, because a retry could apply twice.

## How answers work

Claude reads `profile.md` once per session and answers form questions from it.
If a question isn't covered, Claude asks you rather than guessing, then offers to
save your answer with `save_answer`, so each new question only comes up once.

## Status

Working against the live site (Nov 2026): login check, search, job details, reading
the application form, the submit preview, skipping and the local log.

- Search reads the Next.js Apollo cache on `/role/...` pages.
- Job details come from the Apollo cache when logged in (visa, relocation, skills,
  equity, Wellfound's eligibility check), or from JSON-LD when logged out.
- The apply form opens at `/jobs/<id>-<slug>?autoOpenApplication=true`. Each
  question is a field named `customQuestionAnswers[<key>][answer]`, and a trailing
  `*` marks it required. Wellfound attaches your Wellfound profile itself, so
  nothing is uploaded per application.

Not yet seen live: the moment after "Send application". `submit` treats the form
closing as success and reports an error otherwise. Watch the first real submission.
Only text and textarea questions have been seen so far; select, radio and checkbox
handling is written but untested.

Run the parser tests with `uv run pytest`.
