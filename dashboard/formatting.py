"""Cross-platform presentation helpers shared across dashboard pages."""

from __future__ import annotations

import pandas as pd


def _strip_leading_zero_day(formatted: str) -> str:
    """Turn a leading zero-padded day ("06 Jun 2025") into "6 Jun 2025".

    Avoids the platform-specific ``%-d`` (glibc) / ``%#d`` (Windows) strftime
    flags, which are not portable.
    """
    if formatted.startswith("0"):
        return formatted[1:]
    return formatted


def format_date(value: pd.Series | pd.Timestamp, fmt: str = "%d %b %Y") -> "pd.Series | str":
    """Format a date (or Series of dates) without leading zero on the day.

    Accepts either a single ``Timestamp``/datetime or a pandas Series. The
    output matches the previous ``%-d %b %Y`` rendering but works on Windows.
    """
    if isinstance(value, pd.Series):
        return pd.to_datetime(value, errors="coerce").dt.strftime(fmt).map(
            lambda s: _strip_leading_zero_day(s) if isinstance(s, str) else s
        )
    return _strip_leading_zero_day(pd.Timestamp(value).strftime(fmt))
