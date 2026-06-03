"""Shared UI helpers and constants used across multiple pages."""

import re

import pandas as pd
import plotly.graph_objects as go
import requests
import streamlit as st
from plotly.subplots import make_subplots

from settings import PERIOD_FILTER_DEFAULT, get_period_filter, set_period_filter

_PERIOD_OPTIONS = ["All time", "Last year", "Last 3 months", "Last month"]
_PERIOD_MONTHS: dict[str, int | None] = {
    "All time": None,
    "Last year": 12,
    "Last 3 months": 3,
    "Last month": 1,
}

_GRANULARITY_OPTIONS = ["Year", "Quarter", "Month", "Week", "Day"]
GRANULARITY_FREQ: dict[str, str] = {
    "Year": "Y",
    "Quarter": "Q",
    "Month": "M",
    "Week": "W",
    "Day": "D",
}


# Single shared session_state key holding the selected history range, so the choice
# is the same across every page and survives navigation. Streamlit clears a widget's
# own state once its page unmounts, so we mirror the choice here and seed the widget
# from it. The value is also persisted to settings.json so it survives app restarts.
_PERIOD_STATE_KEY = "_shared_period_value"


def period_filter_pills(key: str) -> int | None:
    """Render a pill-style history-range selector and return months back (or None for all time).

    All callers share one remembered value, persisted to settings.json, so changing the
    range on one page carries over to the others and survives an app restart.
    """
    if _PERIOD_STATE_KEY not in st.session_state:
        stored = get_period_filter()
        st.session_state[_PERIOD_STATE_KEY] = stored if stored in _PERIOD_OPTIONS else PERIOD_FILTER_DEFAULT
    current = st.session_state[_PERIOD_STATE_KEY]
    selected = st.pills("Period", _PERIOD_OPTIONS, default=current, key=key, label_visibility="collapsed")
    chosen = selected or current
    if chosen != current:
        set_period_filter(chosen)
    st.session_state[_PERIOD_STATE_KEY] = chosen
    return _PERIOD_MONTHS.get(chosen)


def granularity_pills(key: str, default: str = "Month", options: list[str] | None = None) -> str:
    """Render a pill-style granularity selector and return the selected label (e.g. 'Month').

    Use GRANULARITY_FREQ to convert the label to a pandas/plotly frequency code.
    Pass `options` to restrict the available choices (subset of _GRANULARITY_OPTIONS).
    """
    opts = options if options is not None else _GRANULARITY_OPTIONS
    selected = st.pills("Period", opts, default=default, key=key, label_visibility="collapsed")
    return selected or default


def months_to_since(months: int | None) -> str | None:
    """Convert a months-back count to an ISO date string for the backend ?since= param."""
    if months is None:
        return None
    return (pd.Timestamp.today() - pd.DateOffset(months=months)).strftime("%Y-%m-%d")

from backend_client import _get as _backend_get
from chart_config import CHART_COLORS
from theme import get_plotly_template

NO_MATCH = "— no match —"
_CO2EQ_KG = "CO₂eq (kg)"


def format_euro(amount: float) -> str:
    """Format a euro amount with Dutch notation: €1.234,56 or €1.234,- for whole amounts."""
    cents = round(amount % 1 * 100)
    thousands = f"{int(amount):,}".replace(",", ".")
    if cents == 0:
        return f"€ {thousands},-"
    return f"€ {thousands},{cents:02d}"

# Semantic similarity scores are 0–100; below this threshold items are flagged for review.
LOW_CONFIDENCE = 40


def ah_product_url(web_id) -> str | None:
    """Build an AH webshop URL from a web_id (numeric)."""
    try:
        wid = int(web_id)
    except (TypeError, ValueError):
        return None
    return f"https://www.ah.nl/producten/product/wi{wid}"


def parse_web_id_input(text) -> int | None:
    """Parse a web_id from a pasted AH product URL or raw integer.

    Accepts:
    - https://www.ah.nl/producten/product/wi123456
    - wi123456
    - 123456
    """
    if text is None:
        return None
    s = str(text).strip()
    if not s:
        return None
    m = re.search(r"wi(\d+)", s)
    if m:
        return int(m.group(1))
    try:
        return int(s)
    except ValueError:
        return None


def render_products_dataframe(products: pd.DataFrame) -> int | None:
    """Render the shared product grid (image + fields) and return selected web_id or None."""
    event = st.dataframe(
        products[
            [
                "thumbnail_url",
                "title",
                "brand",
                "ah_category",
                "unit_size",
                "nutriscore",
                "co2eq_per_kg",
                "co2eq_category",
                "co2eq_per_unit",
            ]
        ].rename(
            columns={
                "thumbnail_url": "Image",
                "title": "Title",
                "brand": "Brand",
                "ah_category": "Category",
                "unit_size": "Unit size",
                "nutriscore": "Nutri-Score",
                "co2eq_per_kg": "CO₂/kg",
                "co2eq_category": "CO₂ category",
                "co2eq_per_unit": "CO₂eq/unit",
            }
        ),
        column_config={
            "Image": st.column_config.ImageColumn("Image", width="small"),
            "CO₂/kg": st.column_config.NumberColumn(format="%.2f"),
            "CO₂eq/unit": st.column_config.NumberColumn(format="%.3f"),
        },
        on_select="rerun",
        selection_mode="single-row",
        width="stretch",
        hide_index=True,
    )
    if event.selection.rows:
        idx = event.selection.rows[0]
        wid = products.iloc[idx]["web_id"]
        if pd.notna(wid):
            return int(wid)
    return None


def render_cost_co2_chart(df: pd.DataFrame) -> None:
    """Render a grouped bar chart of cost and CO₂eq.

    Expects columns: date (datetime for sort), date_fmt (str label), total, co2eq.
    """
    chart_df = df.sort_values("date").copy()
    chart_df["total"] = pd.to_numeric(chart_df["total"], errors="coerce")
    chart_df["co2eq"] = pd.to_numeric(chart_df["co2eq"], errors="coerce")
    if chart_df[["total", "co2eq"]].dropna(how="all").empty:
        st.info("No chartable purchase data is available yet.")
        return
    fig = make_subplots(specs=[[{"secondary_y": True}]])
    fig.add_trace(
        go.Bar(
            x=chart_df["date_fmt"],
            y=chart_df["total"],
            name="Cost (€)",
            marker_color=CHART_COLORS["cost"],
            opacity=0.85,
            offsetgroup=0,
        ),
        secondary_y=False,
    )
    fig.add_trace(
        go.Bar(
            x=chart_df["date_fmt"],
            y=chart_df["co2eq"].round(2),
            name=_CO2EQ_KG,
            marker_color=CHART_COLORS["co2eq"],
            opacity=0.85,
            offsetgroup=1,
        ),
        secondary_y=True,
    )
    fig.update_layout(
        barmode="group",
        bargap=0.15,
        bargroupgap=0.05,
        legend={"orientation": "h", "yanchor": "bottom", "y": 1.02, "xanchor": "right", "x": 1},
        margin={"t": 40, "b": 40},
        height=320,
        template=get_plotly_template(),
    )
    # Force a categorical x-axis so bars are evenly spaced. Without this, Plotly
    # may auto-detect the date-like labels as a continuous time axis (platform
    # dependent), bunching the bars together.
    fig.update_xaxes(type="category")
    fig.update_yaxes(title_text="Cost (€)", secondary_y=False)
    fig.update_yaxes(title_text=_CO2EQ_KG, secondary_y=True)
    st.plotly_chart(fig, width="stretch")


def render_purchase_table(
    df: pd.DataFrame,
    caption: str,
    session_key: str,
) -> int | str | None:
    """Render the shared receipt/order list table and return the selected row's id, or None.

    Expects columns: id, date_fmt, total, co2eq, item_count, matched_pct.
    Optional columns: type, weight, discount (shown when present).
    """
    cols = ["date_fmt", "item_count", "total"]
    rename = {
        "date_fmt": "Date",
        "item_count": "Items",
        "total": "Total",
        "co2eq": _CO2EQ_KG,
        "matched_pct": "Matched",
    }
    if "type" in df.columns:
        cols = ["date_fmt", "type", "item_count", "total"]
        rename["type"] = "Type"
    if "discount" in df.columns:
        cols = cols + ["discount"]
        rename["discount"] = "Discount"
    if "weight" in df.columns:
        cols = cols + ["weight"]
        rename["weight"] = "Weight"
    cols = cols + ["co2eq", "matched_pct"]

    st.caption(caption)
    event = st.dataframe(
        df[cols].rename(columns=rename),
        column_config={
            "Total": st.column_config.NumberColumn(format="€ %.2f"),
            _CO2EQ_KG: st.column_config.NumberColumn(format="%.2f kg"),
            "Weight": st.column_config.NumberColumn(format="%.2f kg"),
            "Discount": st.column_config.NumberColumn(format="€ %.2f"),
        },
        on_select="rerun",
        selection_mode="single-row",
        width="stretch",
        hide_index=True,
        key=session_key,
    )
    if event.selection.rows:
        return df.iloc[event.selection.rows[0]]["id"]
    return None


def _render_sync_status(sync: dict) -> None:
    s_status = sync.get("status", "")
    s_found = sync.get("receipts_found", 0)
    s_synced = sync.get("receipts_synced", 0)
    s_updated = sync.get("updated_at", "")
    if s_status == "running" and s_found > 0:
        st.progress(min(s_synced / s_found, 1.0), text=f"Sync: {s_synced}/{s_found} new receipts")
    elif s_status == "running":
        st.caption("⏳ Sync: starting…")
    elif s_status == "done":
        label = f"{s_synced} new receipts" if s_synced else "up to date"
        st.caption(f"✅ Sync: {label} ({s_updated[:10]})")


def _render_enrich_status(enrich: dict, unenriched: int) -> None:
    e_status = enrich.get("status", "unknown")
    e_total = enrich.get("items_total", 0)
    e_processed = enrich.get("items_processed", 0)
    e_updated = enrich.get("updated_at") or ""
    if e_status == "running" and e_total > 0:
        st.progress(min(e_processed / e_total, 1.0), text=f"Enrichment: {e_processed}/{e_total} items")
    elif unenriched > 0:
        st.caption(f"⏳ Enrichment: {unenriched} item(s) pending")
    else:
        st.caption(f"✅ Enrichment: all items matched ({e_updated[:10]})")


@st.fragment(run_every=3)
def render_pipeline_status() -> None:
    """Auto-refreshing sync + enrichment progress shown in the sidebar."""
    try:
        sync = _backend_get("/sync/status")
        unenriched = _backend_get("/enrichment/pending").get("count", 0)
    except requests.RequestException:
        st.caption("⏳ Waiting for services to start…")
        return

    if sync:
        _render_sync_status(sync)

    try:
        enrich = _backend_get("/status")
        _render_enrich_status(enrich, unenriched)
    except requests.RequestException:
        pass
