"""Theme management — light (AH) / dark startup theming."""

from pathlib import Path

from settings import load_settings, save_settings

DARK = "dark"
LIGHT = "light"

_CONFIG_TOML_PATH = Path.home() / ".streamlit" / "config.toml"

_THEMES: dict[str, dict[str, str]] = {
    DARK: {
        "primaryColor": "#0072CE",
        "backgroundColor": "#0D1B2A",
        "secondaryBackgroundColor": "#162840",
        "textColor": "#E8EDF2",
    },
    LIGHT: {
        "primaryColor": "#0072CE",
        "backgroundColor": "#F5F5F5",
        "secondaryBackgroundColor": "#FFFFFF",
        "textColor": "#1A1A1A",
    },
}


def _write_config_toml(theme: str) -> None:
    c = _THEMES.get(theme, _THEMES[DARK])
    _CONFIG_TOML_PATH.parent.mkdir(parents=True, exist_ok=True)
    _CONFIG_TOML_PATH.write_text(
        "[server]\n"
        'address = "localhost"\n'
        "\n"
        "[browser]\n"
        'serverAddress = "localhost"\n'
        "\n"
        "[theme]\n"
        f'primaryColor             = "{c["primaryColor"]}"\n'
        f'backgroundColor          = "{c["backgroundColor"]}"\n'
        f'secondaryBackgroundColor = "{c["secondaryBackgroundColor"]}"\n'
        f'textColor                = "{c["textColor"]}"\n'
        'font                     = "sans serif"\n'
    )


def progress_bar_colors() -> dict[str, str]:
    """Track/fill/text colors for the custom HTML progress bars (finances, insights),
    resolved against the active theme. In light mode the track is AH blue with a
    darker fill so the white label stays readable across both the filled and
    unfilled portions."""
    if get_theme() == DARK:
        return {"track": "#162840", "fill": "#0072CE", "text": "#E8EDF2"}
    return {"track": "#0072CE", "fill": "#00427A", "text": "#FFFFFF"}


def get_plotly_template() -> str:
    return "plotly_white" if get_theme() == LIGHT else "plotly_dark"


def get_theme() -> str:
    return load_settings().get("theme", DARK)


def set_theme(theme: str) -> None:
    settings = load_settings()
    settings["theme"] = theme
    save_settings(settings)
    _write_config_toml(theme)


def init_theme() -> None:
    """Write config.toml from saved preference. Call once at server startup."""
    _write_config_toml(get_theme())
