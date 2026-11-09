"""jobscout integration: LinkedIn marks in the tracker, career-site fact checks and alerts."""

import json
import sqlite3
import time

from wellfound_mcp import config, dashboard, factcheck, jobscout_log, notify, profile


def _jobscout_db(path, with_apply=True):
    c = sqlite3.connect(path)
    c.executescript(f"""
CREATE TABLE cards (id TEXT PRIMARY KEY, title TEXT NOT NULL, company TEXT);
CREATE TABLE details (id TEXT PRIMARY KEY, title TEXT, company TEXT{', apply TEXT' if with_apply else ''});
CREATE TABLE marks (id TEXT PRIMARY KEY, status TEXT NOT NULL, score INTEGER, reason TEXT, updated_at INTEGER NOT NULL);
""")
    now = int(time.time())
    c.execute("INSERT INTO cards VALUES ('1','Backend Engineer','Acme'), ('2','SDE','Beta'), ('3','Go Dev','Gamma'), ('4','QA','Delta')")
    if with_apply:
        c.execute("INSERT INTO details VALUES ('1','Backend Engineer','Acme','https://jobs.lever.co/acme/1')")
    for id_, st in [("1", "applied"), ("2", "shortlisted"), ("3", "seen"), ("4", "not_interested")]:
        c.execute("INSERT INTO marks VALUES (?,?,70,'why',?)", (id_, st, now))
    c.commit()
    c.close()


def test_rows_map_statuses_and_urls(tmp_path):
    db = tmp_path / "jobscout.db"
    _jobscout_db(db)
    rows = {r["job_id"]: r for r in jobscout_log.rows(db)}
    assert set(rows) == {"1", "2", "4"}                     # "seen" is left out
    assert rows["1"]["status"] == "submitted" and rows["1"]["url"] == "https://jobs.lever.co/acme/1"
    assert rows["2"]["status"] == "shortlisted" and rows["2"]["url"] == "https://www.linkedin.com/jobs/view/2/"
    assert rows["4"]["status"] == "skipped" and rows["4"]["source"] == "linkedin"


def test_rows_old_schema_and_missing_db(tmp_path):
    assert jobscout_log.rows(tmp_path / "none.db") == []
    db = tmp_path / "old.db"
    _jobscout_db(db, with_apply=False)
    assert len(jobscout_log.rows(db)) == 3


def test_dashboard_counts_linkedin(tmp_path, monkeypatch):
    monkeypatch.setattr(config, "DATA_DIR", tmp_path)
    monkeypatch.setattr(config, "DB_PATH", tmp_path / "t.db")
    _jobscout_db(config.JOBSCOUT_DB.parent.mkdir(parents=True) or config.JOBSCOUT_DB)
    t = dashboard.stats()["totals"]
    assert (t["submitted"], t["linkedin"], t["shortlisted"], t["skipped"]) == (1, 1, 1, 1)


def test_check_form_uses_personal_details():
    config.JOBSCOUT_HOME.mkdir(parents=True)
    config.PROFILE_PATH.write_text("Backend engineer, Go and PostgreSQL at Initech.", encoding="utf-8")
    config.PERSONAL_DETAILS.write_text(json.dumps({"_note": "Hooli", "notice_period_days": 30,
                                                   "answers": {"Why switch?": "Growth"}}), encoding="utf-8")
    facts = profile.facts_text()
    ok = factcheck.check_form({"Notice period?": "30 days. I use Go and PostgreSQL at Initech."}, "Acme backend role", facts)
    assert ok["passed"], ok["issues"]
    bad = factcheck.check_form({"Experience": "5 years of Kafka at Hooli."}, "Acme backend role", facts)
    assert not bad["passed"]
    assert any("5" in i for i in bad["issues"]) and any("Kafka" in i for i in bad["issues"])
    assert any("Hooli" in i for i in bad["issues"])        # keys starting with "_" aren't facts


def test_alert_names_the_career_site(monkeypatch):
    monkeypatch.setattr(config, "EMAIL_TO", "me@example.com")
    monkeypatch.setattr(config, "SMTP_USER", "me@example.com")
    msg = notify.build_message({"title": "SDE", "company": "Acme", "url": "https://x"}, [], source="Greenhouse")
    assert msg["Subject"] == "[hireloop] Applied: SDE at Acme"
    assert "on Greenhouse." in msg.get_content() and "wellfound.com" not in msg.get_content()
