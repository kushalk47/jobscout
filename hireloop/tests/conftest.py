"""Keep every test away from the real ~/.jobscout (database, profile, personal details)."""

import pytest

from wellfound_mcp import config


@pytest.fixture(autouse=True)
def _no_real_jobscout(tmp_path, monkeypatch):
    home = tmp_path / "jobscout-home"
    monkeypatch.setattr(config, "JOBSCOUT_HOME", home)
    monkeypatch.setattr(config, "JOBSCOUT_DB", home / "jobscout.db")
    monkeypatch.setattr(config, "PERSONAL_DETAILS", home / "personal_details.json")
    monkeypatch.setattr(config, "PROFILE_PATH", home / "profile.md")
