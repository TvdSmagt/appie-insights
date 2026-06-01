import json
import os
from pathlib import Path

_default_settings_path = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config")) / "appie" / "settings.json"
SETTINGS_PATH = Path(os.environ.get("SETTINGS_PATH", str(_default_settings_path)))


def load_settings() -> dict:
    try:
        with open(SETTINGS_PATH) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def save_settings(settings: dict) -> None:
    os.makedirs(os.path.dirname(SETTINGS_PATH), exist_ok=True)
    with open(SETTINGS_PATH, "w") as f:
        json.dump(settings, f, indent=2)


MA_WINDOW_DEFAULT = 3

_MA_WINDOW_DEFAULTS: dict[str, int] = {
    "Day": 7,
    "Week": 4,
    "Month": 3,
    "Quarter": 4,
    "Year": 3,
}

MA_WINDOW_MAX: dict[str, int] = {
    "Day": 30,
    "Week": 26,
    "Month": 24,
    "Quarter": 8,
    "Year": 5,
}


def get_ma_window_for(granularity: str) -> int:
    key = f"ma_window_{granularity.lower()}"
    return int(load_settings().get(key, _MA_WINDOW_DEFAULTS.get(granularity, MA_WINDOW_DEFAULT)))


def set_ma_window_for(granularity: str, n: int) -> None:
    settings = load_settings()
    settings[f"ma_window_{granularity.lower()}"] = n
    save_settings(settings)


HOUSEHOLD_AE_DEFAULT = 1.0
HOUSEHOLD_AE_KEY = "household_ae"

# Adult-equivalent food consumption weights by age bracket.
# Based on relative caloric intake compared to an adult (18+).
AE_WEIGHTS = {
    "Adults (18+)":       1.0,
    "Teenagers (13–17)":  0.9,
    "Children (6–12)":    0.65,
    "Young children (3–5)": 0.45,
    "Infants (0–2)":      0.3,
}


def get_household_ae() -> float:
    return float(load_settings().get(HOUSEHOLD_AE_KEY, HOUSEHOLD_AE_DEFAULT))


def set_household_ae(ae: float) -> None:
    settings = load_settings()
    settings[HOUSEHOLD_AE_KEY] = ae
    save_settings(settings)


def calculate_ae(counts: dict[str, int]) -> float:
    """Calculate total adult-equivalents from a dict of {bracket_label: count}."""
    return sum(AE_WEIGHTS.get(label, 1.0) * n for label, n in counts.items())


PERIOD_FILTER_DEFAULT = "All time"
PERIOD_FILTER_KEY = "period_filter"


def get_period_filter() -> str:
    return str(load_settings().get(PERIOD_FILTER_KEY, PERIOD_FILTER_DEFAULT))


def set_period_filter(value: str) -> None:
    settings = load_settings()
    settings[PERIOD_FILTER_KEY] = value
    save_settings(settings)
