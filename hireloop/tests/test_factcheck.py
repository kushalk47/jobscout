"""Fit rules and the fact check that gate automatic sending."""

from wellfound_mcp import factcheck

PROFILE = """- Name: Rohan R
- GitHub: github.com/rohxn08
- Current role: AI Engineer Intern, Kenpath Technologies
- Previous: Software Engineer Intern, ABB India
- Education: B.E. in AI and Data Science, GPA 9.48
- Tools: Python, SQL, Pandas, Tableau, Power BI, Qdrant
- ZipRoute: XGBoost ETA over 144,867 trips, MAE 8 min
- Location: Bangalore, India
"""
JOB = {"title": "Data Analyst", "company": "Newton School", "salary": "₹4L – ₹8L", "locations": ["Bengaluru"],
       "stack": ["Excel"], "years": None, "remote": False, "hires_in": [], "visa": "Not available",
       "apply_on_wellfound": True, "eligibility_issues": None, "description": "Fresher / 0–1 Year analytics role."}
QS = [{"key": "1", "question": "What interests you about working for this company?", "required": False},
      {"key": "2", "question": "Your GitHub?", "required": True}]
GOOD = ("Newton School's analytics role fits me. I analysed 144,867 trips in ZipRoute with Pandas and "
        "built Tableau reports; at Kenpath I validated data in Qdrant. I'm based in Bangalore.")


def check(a1, a2="github.com/rohxn08"):
    return factcheck.check({"1": a1, "2": a2}, QS, PROFILE, JOB)


def test_good_answer_passes():
    r = check(GOOD)
    assert r["passed"], r["issues"]


def test_catches_invented_claims():
    assert not check(GOOD.replace("144,867", "200,000"))["passed"]          # number
    assert not check(GOOD.replace("Kenpath", "Google"))["passed"]           # employer
    assert not check(GOOD.replace("Tableau", "Looker"))["passed"]           # tool
    assert not check(GOOD + " See github.com/other.")["passed"]             # foreign link
    assert not check(GOOD.replace("Newton School's", "[Company]'s"))["passed"]  # placeholder
    assert not check("x" * 1600)["passed"]                                  # too long


def test_no_false_alarms_on_ordinary_text():
    job = {**JOB, "company": "timepay.ai"}
    ok = ("timepay.ai is where I want to work. Earlier, I analysed 144,867 trips; "
          "I'm pre-training a ~30M-parameter model.")
    r = factcheck.check({"1": ok, "2": "github.com/rohxn08"}, QS, PROFILE + "\n- FLAME: ~30M params", job)
    assert r["passed"], r["issues"]
    assert not check(GOOD + " Earlier, at Google, I built it.")["passed"]   # names still checked


def test_required_and_unknown_keys():
    assert not check(GOOD, a2="")["passed"]
    r = factcheck.check({"1": GOOD, "2": "github.com/rohxn08", "9": "hi"}, QS, PROFILE, JOB)
    assert any("not on the form" in i for i in r["issues"])


def test_hash_tracks_exact_answers():
    assert factcheck.answers_hash({"1": "a"}) == factcheck.answers_hash({"1": "a"})
    assert factcheck.answers_hash({"1": "a"}) != factcheck.answers_hash({"1": "a "})


def test_fit_rules():
    assert factcheck.fit(JOB)["ok"]
    assert factcheck.fit({**JOB, "years": "3+"})["ok"]                       # 3 is allowed
    assert not factcheck.fit({**JOB, "years": "4+"})["ok"]
    assert not factcheck.fit({**JOB, "description": "Needs 5+ years of SQL"})["ok"]
    assert not factcheck.fit({**JOB, "description": "typically 4–6 years shipping software"})["ok"]
    assert factcheck.fit({**JOB, "description": "Experience: 1–2 years in GenAI"})["ok"]
    assert factcheck.fit({**JOB, "description": "2 to 3 years of Python"})["ok"]
    assert not factcheck.fit({**JOB, "salary": "₹3L – ₹3.6L"})["ok"]
    assert factcheck.fit({**JOB, "salary": "$20k – $25k"})["ok"]             # non-INR not judged
    assert not factcheck.fit({**JOB, "locations": ["Berlin"]})["ok"]
    assert factcheck.fit({**JOB, "locations": ["Berlin"], "visa": "Available"})["ok"]
    assert factcheck.fit({**JOB, "locations": ["Berlin"], "remote": True, "hires_in": ["India"]})["ok"]
    assert not factcheck.fit({**JOB, "apply_on_wellfound": False})["ok"]
    assert not factcheck.fit({"error": "expired"})["ok"]
