# jobscout

**A lightweight, token-frugal MCP server for finding LinkedIn jobs, plus Claude skills that rank the top jobs for your resume and run the whole find → apply → track cycle.**

The repository holds two MCP servers that share `~/.jobscout/`:
- **jobscout** (Go, this folder): LinkedIn search, descriptions, scores and the application tracker.
- **hireloop** ([`hireloop/`](hireloop/README.md), Python): Wellfound search and apply, plus `check_answers` (fact-checks form answers against your profile) and `notify_applied` (email record) for any company career site, a local dashboard and a Sunday report over both.

An unofficial tool. It drives *your own* logged-in Chrome through the Chrome DevTools Protocol: one Go binary, no Python or Node, four tools.

```
you: find me jobs that fit my resume
Claude: profile.md → 6 keywords → jobs_search → screens 120 one-line results → jobs_get 35
        trimmed descriptions → jobs_mark scores → jobs_list top 20
```

## Why it is cheap on tokens

| Where tokens usually go | What jobscout does |
|---|---|
| Tool definitions sent every turn | 4 tools, short descriptions, no output schemas: **≈ 3.2 KB / ~800 tokens** in total (measured in `tools_test.go`) |
| Verbose JSON results | Pipe-separated lines with one header: `id\|title\|company\|location\|posted\|flags` (~25 tokens per job) |
| Same job found by several keywords | One `jobs_search` call runs every keyword and deduplicates by job id |
| Re-reviewing jobs you already saw | `jobs_mark` stores your decisions in local SQLite, and later searches skip them |
| Reading every description | Claude screens on the one-liners first and fetches only promising ids |
| Long descriptions | Company pitch, perks, EEO and "how to apply" sections are removed. Requirements and every eligibility line (years, degree, visa, location, notice period) are kept, and a `signals:` line such as `exp 6+y; degree mentioned; NO visa sponsorship` summarises them. On a real posting this cut ~880 → ~700 tokens; it saves more on postings with long company or perks sections. `full=true` returns the original |
| Fetching the same page twice | Descriptions are cached for 7 days |

The biggest saving is the funnel itself: Claude screens ~25 tokens per job and reads full requirements only for the shortlist. Measure your own runs with `jobscout-mcp bench "keyword one" "keyword two"`.

## Tools

| Tool | Does |
|---|---|
| `jobs_search(keywords[], location, posted_within_days, workplace[], experience[], job_type[], exclude_title_words[], ...)` | Searches every keyword, deduplicates, applies hard filters, skips marked jobs |
| `jobs_get(ids[], full?)` | Trimmed descriptions (≤ 15 per call), cached |
| `jobs_mark(marks[{id, status?, score?, reason?}])` | Batch-saves your evaluation locally; it **never** touches LinkedIn |
| `jobs_list(status[]?, min_score?, since_days?, limit?)` | Ranked shortlist or application tracker |

Statuses: `skipped`, `seen`, `shortlisted`, `applied`, `rejected`, `not_interested`.

`jobs_get` also prints `apply: easy` (LinkedIn Easy Apply) or the company's own apply URL, so the cycle knows which route each job takes.

hireloop's tools are listed in [hireloop/README.md](hireloop/README.md#tools).

## Install

```bash
go install github.com/kushalk47/jobscout/cmd/jobscout-mcp@latest
```
Or download a binary from Releases and put it on your `PATH`.

Building from source needs Go 1.26 or later.

You need Chrome. [Chrome for Testing](https://googlechromelabs.github.io/chrome-for-testing/) is recommended: unzip it to `~/.jobscout/chrome-for-testing/` and it is found automatically. Puppeteer and Playwright Chromium builds, and regular Chrome, are also detected. You can set `chrome.path` instead.

```bash
jobscout-mcp init      # optional: writes ~/.jobscout/config.yaml with every option documented
jobscout-mcp login     # opens a dedicated Chrome profile on LinkedIn's sign-in page; sign in once
jobscout-mcp doctor    # checks Chrome, the debugging port and the login
```

**Signed out?** jobscout can read LinkedIn's public job pages too, but LinkedIn shows a sign-in wall after a few signed-out page loads. jobscout then stops and asks you to run `login`. For real use, sign in once.

Debug commands (they print exactly what the model would see): `jobscout-mcp search "golang developer" "Bengaluru"`, `jobscout-mcp get <job id>`, `jobscout-mcp bench "kw1" "kw2"`.

### Claude Code (server + skill)

```
/plugin marketplace add kushalk47/jobscout
/plugin install jobscout@jobscout
```
Then ask: *"Find me jobs that fit my resume"* and attach or paste your resume, or *"run my job cycle"* to find, apply and track.

The plugin also starts hireloop with `uvx` (needs [uv](https://docs.astral.sh/uv/)); run `uvx --from "git+https://github.com/kushalk47/jobscout#subdirectory=hireloop" playwright install chromium` once, and `... wellfound-mcp-login` if you use Wellfound. Without uv, hireloop simply doesn't start and the skills skip it.

### Your data

Everything personal stays in `~/.jobscout/` and is never part of this repository:

| File | Holds |
|---|---|
| `profile.md` | Skills, experience, targets, hard gates (template: [hireloop/profile.example.md](hireloop/profile.example.md)) |
| `personal_details.json` | Contact details, CTC, notice, education, resume paths, saved and EEO answers |
| `lessons.local.md` | Your standing instructions for the apply cycle (resume per stack, portals to skip) |
| `current_run.md` | The in-progress run |
| `jobscout.db`, `chrome-profile/`, `wellfound/` | Job database, LinkedIn login, Wellfound login and log |
| `.env` | hireloop settings (email alerts, auto mode) |

### Server only (any MCP client)

```bash
claude mcp add jobscout -- jobscout-mcp
```
For other clients, add `{"command": "jobscout-mcp"}` to the client's MCP server list.

## How it works

```
cmd/jobscout-mcp        wiring, subcommands, stdio transport
internal/mcpserver      4 tools → service (the thin "controller")
internal/jobs           use cases: dedupe, hard filters, trimming, compact output (no LinkedIn knowledge)
internal/linkedin       adapter: Chrome over CDP, pacing, host guard, selector table, HTML parsing
internal/store          SQLite (pure Go): cards, cached descriptions, marks
hireloop/               Python MCP: Wellfound, check_answers, notify_applied, dashboard, weekly report
plugin/                 Claude Code plugin: .mcp.json (both servers) + skills/job-hunt, skills/job-apply-cycle
```

- **Chrome:** jobscout attaches to Chrome on `127.0.0.1:9222`, starting it with the dedicated profile `~/.jobscout/chrome-profile` when needed. It uses one tab and never closes your browser.
- **Safety:** it does one page load at a time, waits 4–6 s between loads, opens only `https://www.linkedin.com/jobs/` pages, and is read-only on LinkedIn. It stops at once on a login wall or security checkpoint.
- **When LinkedIn changes its page:** see [docs/FIXING_SELECTORS.md](docs/FIXING_SELECTORS.md). Usually it's one line in the selector table, which you can also override from `config.yaml` without rebuilding.

## Disclaimer

jobscout is not affiliated with LinkedIn. Automating LinkedIn's website is against its User Agreement, and heavy use can get an account restricted. jobscout keeps volume low and paced like a person, and it only reads. You are responsible for how you use it.

## Credits

Job-page logic is ported from [prakharagarwal-dev/linkedin-mcp-server](https://github.com/prakharagarwal-dev/linkedin-mcp-server) (Apache-2.0). See [NOTICE](NOTICE).

`hireloop/` comes from [rohxn08/hireloop](https://github.com/rohxn08/hireloop), merged with its history.

## License

Apache-2.0
