import pandas as pd
import streamlit as st

from backend_client import (
    get_financial_summary,
    get_nutriscore_distribution,
    get_orders,
    get_product_stats,
    get_sustainability_summary,
)
from chart_config import (
    CATEGORY_ICONS,
    CHART_COLORS,
    NUTRISCORE_COLORS,
    REF_AVG_KG_PER_PERSON_MONTH,
    REF_SUSTAINABLE_KG_PER_PERSON_MONTH,
)
from loaders import load_combined_enriched_items, load_receipts
from settings import get_household_ae
from widgets import format_euro, months_to_since, period_filter_pills



# ---------------------------------------------------------------------------
# Data loaders
# ---------------------------------------------------------------------------

def _load_financial_summary(since: str | None = None) -> dict:
    try:
        return get_financial_summary(since=since)
    except Exception:
        return {}


def _load_product_stats(since: str | None = None) -> pd.DataFrame:
    try:
        data = get_product_stats(since=since)
        return pd.DataFrame(data) if data else pd.DataFrame()
    except Exception:
        return pd.DataFrame()


def _load_nutriscore_df(since: str | None = None) -> pd.DataFrame:
    _SCORE_ORDER = ["A", "B", "C", "D", "E"]
    try:
        data = get_nutriscore_distribution(since=since)
        if not data:
            return pd.DataFrame()
        df = pd.DataFrame(data)
        df["score"] = df["score"].fillna("")
        df = df[(df["times_bought"] > 0) & (df["score"] != "")]
        present = set(df["score"])
        return df.set_index("score").reindex(
            [s for s in _SCORE_ORDER if s in present], fill_value=0
        ).reset_index()
    except Exception:
        return pd.DataFrame()


# ---------------------------------------------------------------------------
# Renderers
# ---------------------------------------------------------------------------

def _section_sep() -> None:
    # Intentionally tighter than st.divider() — the dashboard home layout is dense.
    st.markdown(
        "<hr style='margin:6px 0 10px; border:none; border-top:1px solid rgba(128,128,128,0.2);'>",
        unsafe_allow_html=True,
    )


def _section_label(text: str) -> None:
    st.markdown(
        f"<p style='margin:0 0 4px; font-size:0.75rem; font-weight:600; opacity:0.5; "
        f"text-transform:uppercase; letter-spacing:0.08em;'>{text}</p>",
        unsafe_allow_html=True,
    )


def _grade_badge(grade: str) -> str:
    color = NUTRISCORE_COLORS.get(grade, "#888")
    return (
        f'<span style="background:{color}; color:#fff; font-size:1.4rem; font-weight:900; '
        f'border-radius:6px; padding:1px 12px;">{grade}</span>'
    )


def _product_card(title: str, value: str, sublabel: str, thumbnail_url: str | None) -> str:
    img_html = (
        f'<img src="{thumbnail_url}" width="52" height="52" '
        f'style="border-radius:6px; object-fit:contain; background:var(--secondary-background-color,#fff); flex-shrink:0;">'
        if thumbnail_url
        else '<div style="width:52px; height:52px; border-radius:6px; background:rgba(128,128,128,0.15); flex-shrink:0;"></div>'
    )
    safe_title = title.replace("<", "&lt;").replace(">", "&gt;")
    return f"""
<div style="display:flex; align-items:center; gap:10px; padding:10px 12px;
            background:rgba(128,128,128,0.08); border-radius:8px; min-height:72px;">
  {img_html}
  <div style="overflow:hidden; min-width:0;">
    <div style="font-size:1rem; font-weight:800; white-space:nowrap;
                overflow:hidden; text-overflow:ellipsis;">{value}</div>
    <div style="font-size:0.8rem; white-space:nowrap; overflow:hidden;
                text-overflow:ellipsis; opacity:0.85; margin-top:1px;">{safe_title}</div>
    <div style="font-size:0.7rem; opacity:0.5; margin-top:2px;">{sublabel}</div>
  </div>
</div>"""


def _render_overview(n_receipts: int, n_orders: int, n_total_qty: int, n_unique_products: int) -> None:
    c1, c2, c3, c4, c5 = st.columns(5)
    c1.metric("Receipts", n_receipts + n_orders, help=f"{n_receipts} in-store + {n_orders} home delivery")
    c2.metric("In-store", n_receipts)
    c3.metric("Home delivery", n_orders)
    c4.metric("Items bought", n_total_qty, help="Total quantity across all receipts and orders.")
    c5.metric("Unique items", n_unique_products, help="Distinct product lines seen across all receipts and orders.")


def _render_finances(summary: dict, combined: pd.DataFrame) -> None:
    _section_label("Finances")
    c1, c2, c3 = st.columns(3)
    total_spent = combined["amount"].sum()
    c1.metric(
        "Total spent",
        format_euro(total_spent),
        help=(
            "Sum of individual item prices. For in-store receipts, receipt-level discounts "
            "are not subtracted — see the Finances page for the actual amount paid."
        ),
    )
    c2.metric(
        "Discount saved",
        format_euro(summary.get("total_discount", 0)),
        help="Total discount across all receipts and orders",
    )
    c3.metric(
        "Avg / month",
        format_euro(summary.get("avg_per_month", 0)),
        help="Average monthly spending across all recorded months",
    )


def _render_sustainability(sus: dict) -> None:
    _section_label("Sustainability")
    c1, c2, c3, c4 = st.columns(4)

    household_ae = get_household_ae()
    c1.metric("Household", f"{household_ae:.1f} AE", help="Adult-equivalent household size (configured in Settings)")

    with c2:
        grade = sus.get("grade")
        if grade:
            st.markdown(
                f"<p style='margin:0; font-size:0.875rem; opacity:0.6;'>Grade</p>"
                f"{_grade_badge(grade)}",
                unsafe_allow_html=True,
            )
        else:
            st.metric("Grade", "—")

    with c3:
        pct = sus.get("pct_above_sustainable")
        sus_target = REF_SUSTAINABLE_KG_PER_PERSON_MONTH
        if pct is not None:
            if pct >= 0:
                label = f"{pct:.0f}% above target"
                help_text = f"{pct:.0f}% above EAT-Lancet sustainable target ({sus_target:.0f} kg CO₂eq/person/mo)"
            else:
                label = f"{abs(pct):.0f}% below target"
                help_text = f"{abs(pct):.0f}% below EAT-Lancet sustainable target ({sus_target:.0f} kg CO₂eq/person/mo)"
            st.metric("CO₂ vs sustainable", label, help=help_text)
        else:
            st.metric("CO₂ vs sustainable", "—")

    with c4:
        top_cat = sus.get("top_category")
        if top_cat:
            icon = CATEGORY_ICONS.get(top_cat, "🌍")
            st.metric("Top emission category", f"{icon} {top_cat}", help="Category contributing the most CO₂eq")
        else:
            st.metric("Top emission category", "—")


def _render_top_products(stats_df: pd.DataFrame) -> None:
    _section_label("Top products")
    cols = st.columns(4)

    def _top(df: pd.DataFrame, col: str) -> pd.Series | None:
        sub = df.dropna(subset=[col])
        if sub.empty:
            return None
        return sub.nlargest(1, col).iloc[0]

    entries = [
        (_top(stats_df, "times_bought"), lambda r: f"{int(r.times_bought)}×", "Most bought"),
        (_top(stats_df, "total_spent"),  lambda r: format_euro(r.total_spent), "Most spent"),
        (_top(stats_df, "total_kg"),     lambda r: f"{r.total_kg:.1f} kg", "Most weight"),
        (_top(stats_df, "co2eq_total"),  lambda r: f"{r.co2eq_total:.1f} kg CO₂eq", "Highest CO₂eq"),
    ]

    for col, (row, fmt, label) in zip(cols, entries):
        with col:
            if row is not None:
                st.markdown(
                    _product_card(
                        title=row.get("title", "Unknown") if isinstance(row, dict) else row["title"],
                        value=fmt(row),
                        sublabel=label,
                        thumbnail_url=row.get("thumbnail_url") if isinstance(row, dict) else row["thumbnail_url"],
                    ),
                    unsafe_allow_html=True,
                )


def _render_nutriscore(df: pd.DataFrame) -> None:
    _section_label("Nutriscore distribution")
    total = df["times_bought"].sum() or 1
    cols = st.columns(len(df))
    for col, row in zip(cols, df.itertuples()):
        color = NUTRISCORE_COLORS.get(row.score, CHART_COLORS["unknown"])
        pct = row.times_bought / total * 100
        bg = f"linear-gradient(to top, {color}55 {pct:.0f}%, {color}15 {pct:.0f}%)"
        col.markdown(
            f"""<div style="text-align:center; background:{bg}; border-radius:8px; padding:10px 4px;">
              <div style="font-size:1.6rem; font-weight:900; color:{color};">{row.score}</div>
              <div style="font-size:1rem; font-weight:700;">{pct:.0f}%</div>
              <div style="font-size:0.75rem; opacity:0.6;">{row.times_bought}×</div>
            </div>""",
            unsafe_allow_html=True,
        )


# ---------------------------------------------------------------------------
# Page entry point
# ---------------------------------------------------------------------------

def page_dashboard() -> None:
    header_col, filter_col = st.columns([3, 2])
    header_col.header("🏠 Home")
    with filter_col:
        st.write("")
        selected_months = period_filter_pills("dashboard_period")

    receipts = load_receipts()
    if receipts.empty:
        st.info("No receipts synced yet. Run the sync service first.")
        return

    since = months_to_since(selected_months)

    try:
        orders = get_orders()
        orders_df = pd.DataFrame(orders) if orders else pd.DataFrame(columns=["delivery_date"])
        orders_df["delivery_date"] = pd.to_datetime(orders_df["delivery_date"], errors="coerce", utc=True)
    except Exception:
        orders_df = pd.DataFrame(columns=["delivery_date"])

    combined = load_combined_enriched_items()

    # Apply period filter for display stats
    if since:
        cutoff = pd.Timestamp(since, tz="UTC")
        receipts_f = receipts[receipts["date"] >= cutoff]
        orders_f = orders_df[orders_df["delivery_date"] >= cutoff]
        combined_f = combined[combined["date"] >= cutoff]
    else:
        receipts_f, orders_f, combined_f = receipts, orders_df, combined

    _render_overview(
        n_receipts=len(receipts_f),
        n_orders=len(orders_f),
        n_total_qty=int(combined_f["quantity"].sum()),
        n_unique_products=combined_f["description"].nunique(),
    )

    _section_sep()
    _render_finances(_load_financial_summary(since=since), combined_f)

    _section_sep()
    _render_sustainability(get_sustainability_summary(since=since, household_ae=get_household_ae()))

    stats_df = _load_product_stats(since=since)
    if not stats_df.empty:
        _section_sep()
        _render_top_products(stats_df)

    nutriscore_df = _load_nutriscore_df(since=since)
    if not nutriscore_df.empty:
        _section_sep()
        _render_nutriscore(nutriscore_df)
