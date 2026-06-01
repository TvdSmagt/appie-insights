"""Unit tests for dashboard/pages/purchases.py and dashboard/widgets.py."""

import pandas as pd

import pages.purchases as purchases
from widgets import render_cost_co2_chart


def test_render_cost_co2_chart_coerces_non_numeric_values(monkeypatch):
    calls = []

    monkeypatch.setattr("streamlit.plotly_chart", lambda fig, width=None: calls.append((fig, width)))

    df = pd.DataFrame(
        {
            "date": pd.to_datetime(["2026-01-01", "2026-01-02"]),
            "date_fmt": ["1 Jan 2026", "2 Jan 2026"],
            "total": ["12.50", 9.25],
            "co2eq": ["1.234", None],
        }
    )

    render_cost_co2_chart(df)

    assert len(calls) == 1
    chart = calls[0][0]
    assert chart.data[1].y[0] == 1.23
    assert pd.isna(chart.data[1].y[1])


def test_purchase_list_warns_when_sync_is_running(monkeypatch):
    messages = {"warning": [], "info": []}

    monkeypatch.setattr(purchases.st, "session_state", {})
    monkeypatch.setattr(purchases.st, "header", lambda *a, **kw: None)
    monkeypatch.setattr(purchases.st, "warning", lambda msg, *a, **kw: messages["warning"].append(msg))
    monkeypatch.setattr(purchases.st, "info", lambda msg, *a, **kw: messages["info"].append(msg))
    monkeypatch.setattr(purchases, "get_sync_status", lambda: {"status": "running"})
    monkeypatch.setattr(purchases, "load_receipts", lambda: pd.DataFrame())
    monkeypatch.setattr(purchases, "get_orders", lambda: [])

    purchases._purchase_list()

    assert messages["warning"] == [
        "Resync in progress. Purchases may still be changing and charts can be incomplete."
    ]
    assert messages["info"] == ["Purchases are being rebuilt during the current resync."]