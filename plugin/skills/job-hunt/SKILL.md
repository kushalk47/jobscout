---
name: job-hunt
description: Find and rank the LinkedIn jobs a candidate should apply to, from their resume and preferences, using the jobscout MCP tools (jobs_search, jobs_get, jobs_mark, jobs_list). Use when the user asks to find jobs, job hunt, get a shortlist of roles to apply to, or check which jobs fit their resume.
---

# Job hunt with jobscout

Goal: the user's **top 20 jobs to apply to**, each with a score and a one-line reason, for as few tokens as possible.

Token rules (follow them; they are the point of this skill):
- Read the resume **once**, then work from the short profile.
- Screen on the one-line search results; call `jobs_get` only for jobs that survive screening.
- Never ask for `full=true` unless a trimmed description is missing something you need for eligibility.
- Record every decision with **one batched** `jobs_mark` call per step, never one call per job.

## 1. Profile (once)

Look for `~/.jobscout/profile.md`. If it exists and the user did not give a new resume, use it.
Otherwise read the resume and the user's preferences and write `~/.jobscout/profile.md` (about 15 lines):

```
# Candidate
roles: Backend engineer, Go developer
experience_years: 1.5
skills_core: Go, PostgreSQL, REST, Docker
skills_secondary: Kubernetes, Redis, Python
education: B.Tech Computer Science, 2024
location: Bengaluru, India | open to: remote, hybrid | relocate: no
work_authorization: India citizen | needs_sponsorship_abroad: yes
notice_period: 30 days
salary_floor: 12 LPA
avoid: sales, support, QA-only, staffing agencies
seniority_target: entry, associate
```

Ask the user only for fields that decide eligibility and are missing (location, work authorisation, years of experience).

## 2. Keywords

Write 5–8 search phrases a recruiter would put in the job title, for example for a 1.5-year Go backend developer:
`golang developer`, `go backend engineer`, `backend engineer`, `sde 1 backend`, `software engineer backend`, `associate software engineer`.
Mix exact titles with broader ones; do not repeat near-duplicates.

## 3. Search (one call)

```
jobs_search(keywords=[...], location=<profile location>, posted_within_days=14,
            experience=[<from seniority_target>], workplace=[<if the user restricts it>],
            exclude_title_words=["senior","sr","lead","principal","staff","manager","director"])
```
Adjust `exclude_title_words` to the candidate's seniority (drop "senior" for senior candidates).
The answer is one line per job: `id|title|company|location|posted|flags`. Jobs already marked are left out automatically.

## 4. Screen by line

From the lines alone, keep jobs whose title, company and location could plausibly fit. Aim for at most ~40.
Mark the rest in **one** call so they never come back:
`jobs_mark(marks=[{id, status:"skipped"}, ...])`

## 5. Read and judge

Call `jobs_get(ids=[...])` in batches of up to 15. For each job, judge against the profile:

**Hard gates**: if one fails, the score is at most 30.
- Required years of experience more than ~1.5× the candidate's (for example 5+ years against 1.5).
- A degree, clearance or citizenship the candidate does not have.
- Location or onsite requirement the candidate excluded, or visa sponsorship needed but explicitly not offered.

**Score 0–100**:
| weight | factor |
|---|---|
| 40 | core skills overlap with the must-have requirements |
| 20 | experience level fit |
| 15 | role and domain match with target roles |
| 15 | location / workplace / salary fit |
| 10 | nice-to-haves, growth, company signals |

Then save **all** scores of the batch in one call:
`jobs_mark(marks=[{id, score, reason:"Go+PG match; asks 2y vs 1.5y"}, ...])`
Keep reasons under ~12 words, saying the main match and the main gap.

In Claude Code, for more than ~20 jobs you may give each batch to a subagent. The subagent gets `profile.md`, the ids and this rubric, calls `jobs_get` and `jobs_mark` itself, and returns only `id|score|reason`. The full descriptions then never enter the main conversation.

## 6. Shortlist

`jobs_list(min_score=60, limit=20)` → present a table:

| # | score | title | company | location | why | link |
|---|---|---|---|---|---|---|

Link format: `https://www.linkedin.com/jobs/view/<id>/`.
Mark the presented jobs `shortlisted` in one `jobs_mark` call. If fewer than 20 score ≥ 60, say so and suggest broader keywords or a longer `posted_within_days`.

## Later runs

Run steps 2–6 again: searches return only new jobs, so a daily run is cheap.
When the user reports progress, record it: `jobs_mark(marks=[{id, status:"applied"}])`, `rejected`, `not_interested`.
`jobs_list(status=["applied"])` shows their tracker.

## Errors

- "not logged in": ask the user to run `jobscout-mcp login`, sign in in the Chrome window, then retry.
- "layout changed": tell the user LinkedIn changed its page. The fix is the selector table (`selectors:` in `~/.jobscout/config.yaml`, or `internal/linkedin/selectors.go`). Do not retry in a loop.
