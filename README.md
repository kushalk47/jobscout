# wellfound-mcp

An MCP server that lets Claude find and apply to jobs on [Wellfound](https://wellfound.com)
for you. It returns compact JSON so it stays cheap on tokens, browses at human speed,
checks every answer against your profile before sending it, and emails you a record of
everything it does.

- **Search and screen:** searches roles, reads each job, and skips ones that break your rules
  (too much experience, too little pay, wrong country, external apply).
- **Apply:** reads the application form, drafts answers only from your `profile.md`,
  fact-checks them, and submits. With auto mode on, it sends on its own when every check
  passes and holds anything doubtful for you.
- **Track:** an email per application, a held-draft alert, a Sunday report, and a local
  dashboard.

## Setup

Needs Python 3.12–3.14 and Chromium (installed by Playwright). Tested on CachyOS / Arch.

**With uv (recommended):**
```bash
sudo pacman -S uv
git clone https://github.com/rohxn08/wellfound-mcp ~/DEV/wellfound-mcp
cd ~/DEV/wellfound-mcp
uv sync
uv run playwright install chromium
```

**Or with pip:**
```bash
cd ~/DEV/wellfound-mcp
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt        # also installs the wellfound-mcp commands
playwright install chromium
```
With pip, drop the `uv run` prefix from the commands below (e.g. `wellfound-mcp-dashboard`).

Don't use `playwright install --with-deps`: it needs `apt`. If Chromium can't find a
library, install it with pacman.

**Then, once:**
```bash
cp profile.example.md profile.md      # fill it in: background, skills, preferences
cp .env.example .env                  # optional: email alerts and auto mode (see below)
uv run wellfound-mcp-login            # sign in inside the window it opens; it closes itself
```

Sign in **in the window the login command opens**, not your usual browser: that window's
profile is where the session is saved. If "Sign in with Google" is refused, use email and
password or Wellfound's email link.

## Use it from Claude Code

Register the server from the project folder. This uses Claude Code's default **local** scope:
the tools load only when you start `claude` inside this folder, so other projects don't pay
for their tool definitions.
```bash
cd ~/DEV/wellfound-mcp
claude mcp add wellfound -- uv run --directory ~/DEV/wellfound-mcp wellfound-mcp
claude                                 # start Claude Code here to use it
```
(Add `--scope user` instead if you want the tools in every folder, at roughly 1,200 tokens
per message everywhere.) Then ask things like:

- "Find AI engineer and data analyst jobs in Bangalore and apply to the good fits."
- "Show me what you skipped and why."
- "Send the held application to Acme, but shorten the answer."

It runs on your computer, so it works from Claude Code here, not from claude.ai on the web.
Don't run `wellfound-mcp-login` while Claude Code has the server running: Chromium can't
open the same profile twice.

## Tools

| Tool | What it does |
|---|---|
| `check_login` | Checks the saved session is still logged in |
| `get_profile` | Reads `profile.md` |
| `save_answer(question, answer)` | Adds an answer you gave to `profile.md`, so it isn't asked again |
| `search_jobs(role, location?, remote?, limit=10, summaries=false)` | Compact rows; hides skipped jobs, marks ones already drafted, held or applied to. `summaries=true` adds a one-line summary per job |
| `get_job(job_id, full=false)` | Requirements, stack, pay and `fit` against your rules (the fit check always reads the full description). `full=true` also returns the description. Takes the full id from search, like `4417481-software-engineer` |
| `skip_job(job_ids, reason?, confirm=false, undo=false)` | Skips jobs (or un-skips them) |
| `draft_application(job_id)` | Opens the form: its questions, any warnings, `fit`, and the job description for writing answers. Nothing is sent |
| `fact_check(job_id, answers)` | Checks the answers against `profile.md` and the posting |
| `hold_application(job_id, answers, reason)` | Keeps a draft unsent and emails you a "Needs review" alert |
| `submit_application(job_id, answers, confirm=false)` | Preview by default; sends with `confirm=true` |
| `list_applications(status?)` | Your local log: `submitted`, `held`, `drafted` or `skipped` |

## Token use

Results come back as one compact JSON block with empty fields dropped. A search of 10 jobs
is about 400 tokens, checking a job about 90, and the job description (about 250 more) is
only sent with the application form, for jobs you actually apply to. The workflow rules are
stated once in the server's instructions instead of in every tool description.

To keep sessions cheap, start a fresh `claude` session to apply and use `/clear` between
batches. Nothing is lost: the profile, `.env` and the application log live in files.

## Auto mode and the checks

Set `WELLFOUND_AUTO_APPLY=1` in `.env` to let Claude send without asking you each time.
It may only send when **all** of these hold, and the server refuses otherwise:

1. **The job fits your rules:** at most 3 years of experience asked (`WELLFOUND_MAX_YEARS`),
   pay at least ₹4 LPA when listed in rupees (`WELLFOUND_MIN_SALARY_LPA`), in India or with
   visa sponsorship, and applied on Wellfound itself. Jobs that don't fit are skipped with
   the reason.
2. **Claude reviewed every sentence** against your profile.
3. **`fact_check` passed on those exact answers.** It verifies that every number,
   capitalised name (employer, project, tool, place), link and email appears in your
   profile or the posting, that there's no placeholder text, required questions are
   answered, and answers aren't too long.

What the fact check can't catch is true facts combined wrongly (a real project credited to
the wrong employer). That's what step 2 is for. If anything fails or Claude isn't confident,
the application is **held**: not sent, and emailed to you with the draft and the reason.

With `WELLFOUND_AUTO_APPLY=0` (the default), every application waits for you to approve
its preview.

## Pacing and limits

- **Undo window.** After "Send application", Wellfound shows the application as
  **Scheduled** with a 5-minute **Undo** button, then delivers it (status **Pending**).
  Undo any application from [your Applied page](https://wellfound.com/jobs/applications).
  Set `WELLFOUND_WAIT_FOR_SCHEDULED=1` to send one at a time, each after the previous
  one's window closes (off by default).
- **Daily cap.** Starts at 5 a day, rises by 1 for each day you apply without Wellfound
  blocking you, up to 15, and drops back to 5 if Wellfound reports its limit.
- **Wellfound's own limit.** Wellfound caps how many applications can be active at once
  (the number isn't published); slots free up as old ones expire. If you hit it, the tools
  return `limit_reached: true` with Wellfound's message, and Claude stops.
- **Human speed.** Each page load waits 3–7s at random, then scrolls and moves the mouse.
  Answers are typed with uneven timing, and the mouse moves to buttons before clicking.
- **Retries.** Failed page loads (network errors, 429, 5xx) are retried up to 3 times with
  backoff. If Cloudflare shows a check, the server waits up to 2 minutes for you to clear it
  in the browser window. "Send application" is never retried, so nothing is sent twice.

## Email alerts

Every submitted application emails you the role, company, link, pay, location, time and
your answers (`[Wellfound] Applied: …`). Held drafts send `[Wellfound] Needs review: …`.

1. On the Gmail account that sends the alerts, turn on 2-Step Verification, then create an
   **app password** at https://myaccount.google.com/apppasswords. Use that 16-character
   code, not your Gmail or Wellfound password.
2. In `.env`, fill in `WELLFOUND_EMAIL_TO` and `WELLFOUND_SMTP_PASSWORD`. Leave
   `WELLFOUND_SMTP_USER` blank to send from that address to itself.
3. `uv run wellfound-mcp-test-email` sends a sample alert.

A failed email never undoes an application; the tool result just says the email failed.
Filter on `subject:"[Wellfound]"` in Gmail to label everything.

## Weekly report

A Sunday 18:00 email with the week's applications and links, held drafts, unsent drafts,
skip reasons and your current daily cap.

```bash
uv run wellfound-mcp-weekly-report --dry-run    # preview this week's report
uv run wellfound-mcp-weekly-report --install    # set up the Sunday timer (systemd user)
uv run wellfound-mcp-weekly-report --uninstall  # remove it
```

The timer is persistent: if you're logged out on Sunday evening, the report goes out the
next time you log in. Each week is sent at most once.

## Dashboard

```bash
uv run wellfound-mcp-dashboard
```

This opens http://127.0.0.1:8765 in your browser (stop it with Ctrl+C). It shows totals,
today's usage against the cap, applications per day for 30 days, the companies you applied
to most, skip reasons, and a filterable list of every job with links. It reads the local log
directly, so **it doesn't need the MCP registered or running**. It only listens on your own
machine and never changes anything. Use `--no-open` to skip opening the browser, or
`WELLFOUND_DASHBOARD_PORT` for another port.

Recruiter replies only show on Wellfound's Applied page; the dashboard knows what the MCP
logged on this computer.

## Settings

Put these in `.env` (gitignored) or the environment; real environment variables win.

| Variable | Default | Purpose |
|---|---|---|
| `WELLFOUND_AUTO_APPLY` | `0` | `1` lets Claude send when every check passes |
| `WELLFOUND_MAX_YEARS` | `3` | Most experience a job may ask for |
| `WELLFOUND_MIN_SALARY_LPA` | `4` | Lowest pay (in lakhs a year) when the job lists pay in rupees |
| `WELLFOUND_DAILY_START` / `WELLFOUND_DAILY_LIMIT` | `5` / `15` | Daily cap: where it starts and how high it can rise |
| `WELLFOUND_WAIT_FOR_SCHEDULED` | `0` | `1` waits for the previous application's undo window before sending the next |
| `WELLFOUND_EMAIL_TO` | | Where alerts and reports go |
| `WELLFOUND_SMTP_USER` | `WELLFOUND_EMAIL_TO` | Account that sends them |
| `WELLFOUND_SMTP_PASSWORD` | | Gmail app password for that account |
| `WELLFOUND_HEADLESS` | `0` | `1` hides the browser (Cloudflare checks then can't be cleared) |
| `WELLFOUND_MIN_DELAY` / `WELLFOUND_MAX_DELAY` | `3` / `7` | Random pause between page loads, in seconds |
| `WELLFOUND_DASHBOARD_PORT` | `8765` | Dashboard port |
| `WELLFOUND_MCP_DATA` | `./data` | Browser session and application log |
| `WELLFOUND_PROFILE` | `./profile.md` | Your profile |

## Your data

Everything stays in the project folder, and `.gitignore` keeps the private parts out of git:

| Path | What it holds |
|---|---|
| `profile.md` | Your background and standard answers (template: `profile.example.md`) |
| `.env` | Settings, including your Gmail app password (template: `.env.example`) |
| `data/browser-profile/` | Your logged-in Wellfound session. Treat it like a password |
| `data/applications.db` | The log of submitted, held, drafted and skipped jobs |

## Troubleshooting

| Problem | Fix |
|---|---|
| `check_login` says logged out | Close Claude Code, run `uv run wellfound-mcp-login`, sign in |
| "browser profile is already open" | Close the login window (or Claude Code) and retry |
| "no display found" | Start Claude Code from your desktop session, or set `DISPLAY` |
| A Cloudflare check appears | Clear it in the browser window within 2 minutes |
| No alert emails | Run `uv run wellfound-mcp-test-email`; Gmail needs an app password |
| Applied page shows "Scheduled" | Normal: Wellfound delivers it after the 5-minute undo window |

## How it reads Wellfound

Checked against the live site in Nov 2026:

- **Search** reads the Next.js Apollo cache on `/role/<role>`, `/role/r/<role>` (remote) and
  `/role/l/<role>/<city>` pages.
- **Job pages** are read from the Apollo cache when logged in (visa, relocation, skills,
  equity, Wellfound's eligibility check), or from schema.org JSON-LD when logged out.
  Expired jobs return 410.
- **The apply form** opens at `/jobs/<id>-<slug>?autoOpenApplication=true`. Each question is
  a field named `customQuestionAnswers[<key>][answer]`; a trailing `*` marks it required.
  Wellfound attaches your Wellfound profile itself, so nothing is uploaded per application.
- **After sending**, the form closes and the application appears as Scheduled, then Pending.

Only text and textarea questions have appeared so far; select, radio and checkbox handling
is written but untested.

## Development

```bash
uv sync
uv run pytest        # offline tests: parsers, fact check, store, alerts, dashboard, report
```
