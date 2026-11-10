"""Settings, read from environment variables and an optional .env file."""

import os
from pathlib import Path

BASE_URL = "https://wellfound.com"

# Everything lives inside the project folder, and private files are gitignored.
PROJECT_ROOT = Path(__file__).resolve().parents[2]


def _load_dotenv(path: Path) -> None:
    """Read KEY=VALUE lines from .env (gitignored). Real env vars win."""
    if not path.is_file():
        return
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip("'\""))


# Shared with jobscout: one home for profile, personal details and logs.
JOBSCOUT_HOME = Path(os.environ.get("JOBSCOUT_HOME", Path.home() / ".jobscout")).expanduser()


def _local_or_shared(local: Path, shared: Path) -> Path:
    """Prefer the project-local file when it exists (older setups), else ~/.jobscout."""
    return local if local.exists() else shared


_load_dotenv(PROJECT_ROOT / ".env")
_load_dotenv(JOBSCOUT_HOME / ".env")

# ./data if it exists, else ~/.jobscout/wellfound. Holds your logged-in browser
# profile (treat it like a password) and the local application log.
DATA_DIR = Path(os.environ.get("WELLFOUND_MCP_DATA") or _local_or_shared(PROJECT_ROOT / "data", JOBSCOUT_HOME / "wellfound")).expanduser()

# jobscout's LinkedIn database (read-only here: the dashboard and weekly report
# include its applied/shortlisted/skipped marks) and its personal details file
# (used by check_answers alongside profile.md).
JOBSCOUT_DB = Path(os.environ.get("JOBSCOUT_DB") or JOBSCOUT_HOME / "jobscout.db").expanduser()
PERSONAL_DETAILS = Path(os.environ.get("JOBSCOUT_PERSONAL_DETAILS") or JOBSCOUT_HOME / "personal_details.json").expanduser()
PROFILE_DIR = DATA_DIR / "browser-profile"
DB_PATH = DATA_DIR / "applications.db"

# Visible browser by default: bot checks are less likely to trip, and you
# can watch what it does. Set WELLFOUND_HEADLESS=1 to hide it.
HEADLESS = os.environ.get("WELLFOUND_HEADLESS", "0") == "1"

# Seconds to wait between page loads, picked at random in this range.
MIN_DELAY = float(os.environ.get("WELLFOUND_MIN_DELAY", "3"))
MAX_DELAY = float(os.environ.get("WELLFOUND_MAX_DELAY", "7"))

# The daily cap ramps up: it starts at DAILY_START, rises by 1 for every past day
# you applied without hitting Wellfound's limit on active applications, and stops
# at DAILY_MAX. Hitting Wellfound's limit drops it back to DAILY_START.
DAILY_START = int(os.environ.get("WELLFOUND_DAILY_START", "5"))
DAILY_MAX = int(os.environ.get("WELLFOUND_DAILY_LIMIT", "15"))

# Your background and standard answers, used to fill application forms.
# ./profile.md if it exists, else ~/.jobscout/profile.md (the one jobscout's
# skills use). Copy profile.example.md there and fill it in. Never commit it.
PROFILE_PATH = Path(os.environ.get("WELLFOUND_PROFILE") or _local_or_shared(PROJECT_ROOT / "profile.md", JOBSCOUT_HOME / "profile.md")).expanduser()

# Email alert after each submitted application. Gmail needs an app password
# (Google Account > Security > App passwords), not your normal password.
# Put these in .env; it's gitignored. Alerts are off until both are set.
EMAIL_TO = os.environ.get("WELLFOUND_EMAIL_TO", "")
SMTP_USER = os.environ.get("WELLFOUND_SMTP_USER", "") or EMAIL_TO
SMTP_PASSWORD = os.environ.get("WELLFOUND_SMTP_PASSWORD", "").replace(" ", "")
SMTP_HOST = os.environ.get("WELLFOUND_SMTP_HOST", "smtp.gmail.com")
SMTP_PORT = int(os.environ.get("WELLFOUND_SMTP_PORT", "465"))

# Automatic applying. When on, Claude may submit without asking, but only for
# jobs that pass the fit rules and answers that pass fact_check; everything
# else is held and emailed to you. Off unless WELLFOUND_AUTO_APPLY=1.
AUTO_APPLY = os.environ.get("WELLFOUND_AUTO_APPLY", "0") == "1"
MAX_YEARS = float(os.environ.get("WELLFOUND_MAX_YEARS", "3"))
MIN_SALARY_LPA = float(os.environ.get("WELLFOUND_MIN_SALARY_LPA", "4"))

# Off by default. When on (=1), send one application at a time: wait until the previous one
# has left Wellfound's 5-minute "Scheduled" (undo) window, give or take SLACK
# seconds. One tool call waits at most MAX_WAIT seconds, then asks to retry.
WAIT_FOR_SCHEDULED = os.environ.get("WELLFOUND_WAIT_FOR_SCHEDULED", "0") == "1"
SCHEDULED_SLACK_S = int(os.environ.get("WELLFOUND_SCHEDULED_SLACK_S", "15"))
SCHEDULED_MAX_WAIT_S = int(os.environ.get("WELLFOUND_SCHEDULED_MAX_WAIT_S", "330"))
