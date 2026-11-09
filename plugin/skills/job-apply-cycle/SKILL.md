---
name: job-apply-cycle
description: Run the whole job cycle end to end - find fresh LinkedIn (jobscout) and Wellfound (hireloop) jobs, score them against the resume, list them split by apply route, then fill, fact-check and submit applications on company career sites under the user's supervision. Use when the user says "run my job cycle", "find and apply to jobs", "apply to new jobs this week", or similar.
---

# Job cycle: find → list → apply → track

One run = jobs posted in the **last 7 days** (unless the user says otherwise), scored, listed, and applied to.
Tools:
- **jobscout** (LinkedIn): `jobs_search`, `jobs_get`, `jobs_mark`, `jobs_list`.
- **hireloop** (Wellfound + checks): `search_jobs`, `get_job`, `draft_application`, `fact_check`, `submit_application`, `hold_application`, plus `check_answers` and `notify_applied` for any career-site form. If hireloop isn't connected, skip the Wellfound parts and do the fact check by hand.
- A browser for company sites: Claude in Chrome first, chrome-devtools MCP as fallback.

## Files (read once per run, never ask the user to repeat them)

All personal data lives in `~/.jobscout/` on the user's machine and never goes into a repository.

- `~/.jobscout/current_run.md` (if present): the in-progress run: what was submitted, what is half-filled, what waits on the user. Resume from it instead of searching again, and keep it updated.
- `~/.jobscout/profile.md`: skills, experience, targets, hard gates, which resume suits which job.
- `~/.jobscout/personal_details.json`: contact details, CTC, notice, education, resume paths, saved answers, EEO answers, legal declarations.
  If a field is `null` and a form needs it, ask the user once and **write the answer back** into the file.
  Save reusable answers to recurring questions under `answers`.
  Never ask for something that is already in this file or in `profile.md`.
- `~/.jobscout/lessons.local.md` (if present): the user's standing instructions and personal lessons (preferred resumes per stack, portals to skip, sites they stay signed in to). These override the defaults below.
- Resume paths in `personal_details.json` are absolute (usually under `~/.jobscout/resumes/`).

## Phase 1: find (token-cheap)

1. Write 6-8 title-style keywords from `profile.md` (mix exact titles and broader ones, and every stack the user has real experience in).
2. `jobs_search(keywords, location="India" or profile location, posted_within_days=7, most_recent=true, experience=["entry","associate"], exclude_title_words=[senior, sr, lead, principal, staff, manager, architect, intern, frontend, qa, test, android, ios, devops, ...])`.
   Jobs marked earlier are skipped automatically, so each run only sees new postings.
3. Screen on the one-line results and keep plausible titles. Mark the rest `skipped` in **one** `jobs_mark` call.
4. `jobs_get` the survivors in batches of up to 15 (it is paced; ~15 s per job is normal). Each result has:
   - `signals:` (years asked, sponsorship), used for hard gates.
   - `apply:` either `easy` (LinkedIn Easy Apply) or the company's own apply URL.
5. **Wellfound (hireloop):** `check_login`, then `search_jobs(role, location)` for the same 2-3 core titles. `get_job` the plausible ones; `skip_job` those with `fit.ok` false, using the fit reasons (with the user's OK unless auto mode is on).
6. Score 0-100. These hard gates cap the score at 30:
   - asks for more than about 2× the candidate's years of experience
   - wrong stack or role type (for example Android, Java-only, QA)
   - excluded location
   - an apply portal the user chose to skip (see `lessons.local.md`; for example Workday `*.myworkdayjobs.com`). Mark those `not_interested` with the portal as the reason and leave them out of the lists.

   Weights: core-skill match 40, experience fit 20, role/domain 15, location/salary 15, extras 10.
   Save all LinkedIn scores in **one** `jobs_mark` call (`shortlisted` if ≥ 60, else `seen`).
7. Also re-check jobs shortlisted in earlier runs: `jobs_list(status=["shortlisted"])`. Keep only those still posted within 7 days and not yet applied.

## Phase 2: list (show before applying)

Present four tables, best first, each row with score, role, company, posted age, and why:
1. **LinkedIn Easy Apply**: the user applies themselves (LinkedIn forbids automated applying). Give `https://www.linkedin.com/jobs/view/<id>/`.
2. **Wellfound**: applied through hireloop's own browser, no Chrome needed.
3. **Company sites, no account needed**: Lever, Ashby, Greenhouse, Recruitee, SmartRecruiters, Deel, Google Forms, Zoho and similar. Claude applies.
4. **Company sites that need an account**: Workday, amazon.jobs, Google, Microsoft, iCIMS, SuccessFactors, Phenom-based portals, cutshort and similar. The user signs in or creates the account; Claude fills the rest.

## Phase 3: apply (supervised)

Claude fills the form, attaches the resume and clicks Submit itself once every answer comes from the files and passes the fact check. The user steps in only for CAPTCHAs, sign-ins/account creation, recordings (videos), codes sent by email, and questions whose answers are not in `personal_details.json` / `profile.md`.
Never apply through LinkedIn itself, and never click LinkedIn Easy Apply.

### Wellfound jobs

`draft_application(job_id)` → write answers only from the profile → review every sentence yourself (right fact credited to the right employer/project) → `fact_check` → `submit_application` (preview, then `confirm=true` after the user's OK, or directly in auto mode). If anything fails or you're unsure, `hold_application` with the reason. Stop when the daily cap or Wellfound's active-application limit is reached.

### Company career sites

Prefer Claude in Chrome (`mcp__claude-in-chrome__*`, the user's real Chrome, has `file_upload`). CAPTCHA-protected forms (Lever, Ashby) reject submissions from a remote-controlled browser, so use the chrome-devtools MCP browser (`mcp__plugin_chrome-devtools-mcp_chrome-devtools__*`, has `upload_file`) only for forms without a CAPTCHA.

For each job, highest score first:
1. Open the `apply:` URL **in a new tab** (`tabs_create_mcp`). If the job is closed, mark it `not_interested` with reason "closed" and move on.
2. **Already signed in?** Open the job and check first; if the site shows the user as signed in, go straight to filling.
   Claude never submits a login form, even one Chrome has autofilled. If the session has expired, the user signs in.
3. **Sign-in or account wall:** open the site's sign-in page in its own new tab (batch all such sites together) and tell the user "Please sign in on <site>, then say done." The user clicks any Google/SSO button and consent screen themselves.
   Claude never types passwords, never creates accounts, and never solves CAPTCHAs, even if the user offers credentials in chat.
4. **Fill** the form from `personal_details.json` and `profile.md`:
   - Upload the right resume for the stack (see `lessons.local.md`). `upload_file` only accepts files inside the session's working folder; copy the resume there first if the path is refused.
   - Answer free-text questions in 2-4 honest sentences grounded in the resume. Never invent experience or skills.
   - Diversity/EEO questions: use `eeo_answers`. Legal declarations (citizenship, PR, non-compete, government employment) come only from the file, never from guesses.
   - Phone fields: always type the number with the country code (`+91 ...`). A bare number can be misread as another country's code.
   - Never enter government IDs (Aadhaar, PAN, passport) or bank details; ask the user to type those.
   - **Cross-origin iframe forms** (for example mynexthire widgets): Claude in Chrome can't read or upload into them. Use the chrome-devtools MCP browser unless the form has a CAPTCHA.
   - **Google Forms file questions** go through Google's Drive picker, which no tool can upload into. The user clicks Add file → Browse → the resume. Fill everything else first.
   - **Cookie banners:** accept only when the site hides the form until cookies are accepted; otherwise decline non-essential.
5. **Fact-check before Submit:** `check_answers(answers={question label: answer}, job_text=<title, company and description>)` on every free-text answer. Fix each issue from the files, or leave that question for the user. Self-ratings, frameworks and NDAs are the user's call; ask once, then save the answer.
6. **Submit:** click Submit once every required field is filled and checked. Record a short summary (site, resume, key field values, free-text answers) in `current_run.md`.
   If a CAPTCHA appears, ask the user to solve it in that tab, then click Submit (or let the user).
   If a required field needs something not in the files (a video, an unknown fact), leave that tab for the user and move on.
7. **Verify every submit.** Read the tab (`get_page_text` / `find`, or a screenshot) and look for the site's own success page:
   Lever "Application submitted", Ashby "Thanks for applying" / "successfully submitted", Recruitee "All done!" in the *visible* panel (check the form is gone), Google Forms "Your response has been recorded", Zoho "applied successfully", SuccessFactors "Process completed".
   - If the page still shows the form, an error, or an empty CAPTCHA, it was **not** submitted. Tell the user and do not mark it applied.
   - **Leave the success tab open.** Never reuse or close it; open a new tab for the next application. List success tabs (title + URL) under "Applied tabs" in `current_run.md` so the user can group them.
   - Many sites send no confirmation email; a missing email is not proof of failure, but an on-page success message is required before marking.
8. Only after that check: `jobs_mark(marks=[{id, status:"applied", reason:"applied via <site> <date>, success page seen"}])` (or "submitted by user, unverified"), then `notify_applied(title, company, url, site, answers)` for the user's email record.

## End of run

Report:
- what was applied to (LinkedIn/career sites and Wellfound)
- what is waiting on the user (sign-ins, Easy Apply links, held Wellfound drafts, questions)
- what was skipped and why

Trackers: `jobs_list(status=["applied"])`, `list_applications(status="submitted")`, and the local dashboard (`wellfound-mcp-dashboard`, http://127.0.0.1:8765), which shows both.

## Errors

- **LinkedIn "sign-in" error:** the user runs `jobscout-mcp login` and signs in; then retry.
- **"layout changed":** LinkedIn changed its page. Fix the selectors (see `docs/FIXING_SELECTORS.md`). Don't loop.
- **Wellfound logged out:** the user closes Claude Code and runs `wellfound-mcp-login`.
- **A form field Claude can't operate:** hand that field to the user and continue with the next one.
- **Auto-mode safety checks** may block a final Submit or a download. Don't work around them; tell the user and move on.

## ATS notes (newest first)

- **Email applications (Gmail):** only when the user allows it, and show the drafted text first. Open compose with `?view=cm&fs=1` and keep personal data out of the URL. The real body is `div[aria-label="Message Body"][contenteditable]` (a hidden textarea shares the label). Type the body once: a retried `type` lands mid-text. Attach via the hidden `input[type=file]`, then check for "Message sent".
- **SuccessFactors:** upload the resume FIRST: uploading re-renders the page and wipes typed answers. "Years" fields take numbers only; textareas max 200 chars; Yes/No radios appear only after the first failed submit, so re-read the page after Apply. Success shows "Process completed" / title "Successfully Applied".
- **Only the active Chrome tab takes `computer` clicks.** Background tabs (and minimized windows) report a 0x0 viewport: screenshots time out and clicks silently do nothing. Fill those with JS (`el.click()`, native value setter + input/change events). A tab from `tabs_create_mcp` is active, so do click-heavy forms right after creating it. Tab IDs can change on reload; call `tabs_context_mcp` again.
- **Greenhouse:** country picker: type "India +91" and press Enter. Some boards (DeepIntent, Stripe) email an 8-character code before submitting; the user enters it. Embedded boards on company sites are reachable at `https://job-boards.greenhouse.io/embed/job_app?for=<board>&token=<gh_jid>`.
- **Deel** (jobs.deel.com): the dial-code dropdown doesn't open from automation; phone is optional, so clear it. Textareas come in pairs (a hidden shadow per question), so fill every other one. Apply stays disabled until every radio (residence, notice) is answered.
- **Flexiple:** "Log in" signs in with a saved session. On the review step, Edit Resume replaces the profile resume; untick "Update my work history".
- **Infor (Pinpoint)** holds submits behind reCAPTCHA; fill everything, then hand Submit to the user.
- **Find no-account jobs directly from ATS APIs:** `boards-api.greenhouse.io/v1/boards/<board>/jobs` (`/<id>?questions=true` for the form), `api.lever.co/v0/postings/<company>?mode=json`, `api.ashbyhq.com/posting-api/job-board/<org>`. Filter titles and India locations, then read the years in the description.
- **Ashby success** is a green banner, "Your application was successfully submitted"; `get_page_text` right after the click can miss it, so screenshot.
- **Phone pickers (intl-tel-input):** typing "India" highlights "British Indian Ocean Territory +246" and truncates the number. Click the "India +91" option and check the code.
- **React-select dropdowns:** typing "No" + Enter can pick an option containing "now". Open the list, click the exact option, read the values back.
- **Lever:** fill fields with JS by `name` (`name`, `email`, `phone`, `org`, `urls[LinkedIn]`, `cards[...][fieldN]`) after uploading the resume.
- **LaTeX resumes without a local TeX install:** POST the source to `https://www.overleaf.com/docs` (`snip`, `snip_name`, `engine=pdflatex`) from an overleaf.com tab where the user is signed in; check the page count, then Download PDF.
- **Volume over polish:** don't spend 30+ tool calls on one slow portal. Prefer one-page forms (Lever, Ashby, Greenhouse, Recruitee, Zoho, Google Forms, cutshort, Flexiple, mynexthire).
- **Job-portal profiles keep stale values** (pay, notice). Check the review step against `personal_details.json` before submitting.
- **"Have you previously applied to <company>?"** is honestly Yes from the second application onward.
- **AI-data or personalization consent prompts:** choose the privacy-preserving "No".
