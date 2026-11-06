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
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip("'\""))


_load_dotenv(PROJECT_ROOT / ".env")

# ./data by default. Holds your logged-in browser profile (treat it like a
# password) and the local application log.
DATA_DIR = Path(os.environ.get("WELLFOUND_MCP_DATA", PROJECT_ROOT / "data"))
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
# Copy profile.example.md to profile.md and fill it in. Gitignored, because it
# holds personal details.
PROFILE_PATH = Path(os.environ.get("WELLFOUND_PROFILE", PROJECT_ROOT / "profile.md"))

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

