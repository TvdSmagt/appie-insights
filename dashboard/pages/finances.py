import pandas as pd
import plotly.graph_objects as go
import streamlit as st

from backend_client import get_financial_summary, get_spending_by_category, get_spending_over_time, get_top_discounts
from chart_config import CHART_COLORS
from settings import MA_WINDOW_MAX, get_ma_window_for, set_ma_window_for
from theme import get_plotly_template, progress_bar_colors
from widgets import format_euro, granularity_pills, months_to_since, period_filter_pills

_FINANCES_GRANULARITIES = ["Year", "Quarter", "Month", "Week", "Day"]



def _load_summary(since: str | None = None) -> dict:
    try:
        return get_financial_summary(since=since)
    except Exception as e:
        st.error(f"Could not load financial summary: {e}")
        return {}


def _load_category_df(since: str | None = None) -> pd.DataFrame:
    try:
        data = get_spending_by_category(since=since)
    except Exception as e:
        st.error(f"Could not load category data: {e}")
        return pd.DataFrame()
    if not data:
        return pd.DataFrame()
    return pd.DataFrame(data)


def _load_top_discounts_df(since: str | None = None) -> pd.DataFrame:
    try:
        data = get_top_discounts(since=since)
    except Exception as e:
        st.error(f"Could not load discount data: {e}")
        return pd.DataFrame()
    if not data:
        return pd.DataFrame()
    return pd.DataFrame(data)


def _load_over_time_df(period: str, since: str | None = None) -> pd.DataFrame:
    try:
        data = get_spending_over_time(period.lower(), since=since)
    except Exception as e:
        st.error(f"Could not load spending over time: {e}")
        return pd.DataFrame()
    if not data:
        return pd.DataFrame()
    return pd.DataFrame(data)


def _render_summary_metrics(summary: dict) -> None:
    c1, c2, c3, c4 = st.columns(4)
    c1.metric("Total spent", format_euro(summary.get("total_spent", 0)))
    c2.metric("Avg / year",  format_euro(summary.get("avg_per_year", 0)))
    c3.metric("Avg / month", format_euro(summary.get("avg_per_month", 0)))
    c4.metric("Avg / week",  format_euro(summary.get("avg_per_week", 0)))

    d1, d2, d3, d4 = st.columns(4)
    d1.metric("Total discount", format_euro(summary.get("total_discount", 0)))
    d2.metric("Avg / year",     format_euro(summary.get("discount_avg_per_year", 0)))
    d3.metric("Avg / month",    format_euro(summary.get("discount_avg_per_month", 0)))
    d4.metric("Avg / week",     format_euro(summary.get("discount_avg_per_week", 0)))


def _render_spending_chart(period: str, since: str | None = None) -> None:
    df = _load_over_time_df(period, since=since)
    if df.empty:
        st.info("No spending data yet.")
        return

    show_col, ma_col = st.columns([3, 1])
    with show_col:
        show = st.pills(
            "Show",
            ["Spent", "Discount"],
            selection_mode="multi",
            default=["Spent", "Discount"],
            key="finances_chart_show",
        )
    with ma_col:
        st.caption(f"{period} moving avg")
        window = get_ma_window_for(period)
        new_window = st.number_input(
            "MA window",
            min_value=1,
            max_value=MA_WINDOW_MAX.get(period, 12),
            value=window,
            key=f"finances_ma_{period}",
            label_visibility="collapsed",
        )
        if new_window != window:
            set_ma_window_for(period, int(new_window))
            window = int(new_window)

    df["moving_avg"] = df["amount"].rolling(window, min_periods=1).mean()

    template = get_plotly_template()
    fig = go.Figure()

    if "Spent" in show:
        fig.add_bar(x=df["period"], y=df["amount"], name="Spent", marker_color=CHART_COLORS["spent"], opacity=0.75)
    if "Discount" in show:
        fig.add_bar(x=df["period"], y=df["discount"], name="Discount", marker_color=CHART_COLORS["discount"], opacity=0.75)
    if "Spent" in show:
        fig.add_scatter(
            x=df["period"],
            y=df["moving_avg"],
            mode="lines",
            name=f"{window}-period avg",
            line={"color": CHART_COLORS["moving_avg"], "width": 2},
        )

    fig.update_layout(
        barmode="stack",
        template=template,
        xaxis_title=period,
        yaxis_title="Amount (€)",
        yaxis_tickprefix="€",
        legend={"orientation": "h", "yanchor": "bottom", "y": 1.02, "xanchor": "right", "x": 1},
        margin={"t": 40, "b": 40},
    )
    st.plotly_chart(fig, width="stretch")


def _progress_bar_html(ratio: float, label: str) -> str:
    pct = ratio * 100
    c = progress_bar_colors()
    return f"""
<div style="position:relative; background:{c['track']}; border-radius:6px; height:28px; margin:4px 0; overflow:hidden;">
  <div style="background:{c['fill']}; width:{pct:.1f}%; height:100%;"></div>
  <div style="position:absolute; inset:0; display:flex; align-items:center; padding:0 10px; font-size:13px; white-space:nowrap; overflow:hidden;">
    <span style="color:{c['text']}; font-weight:700;">{label}</span>
  </div>
</div>"""


def _render_top5(df: pd.DataFrame, name_col: str, title: str, value_col: str = "total_spent") -> None:
    st.subheader(title)
    top = df.nlargest(5, value_col).reset_index(drop=True)
    if top.empty:
        st.info("No data.")
        return
    max_val = top[value_col].max()
    for row in top.itertuples():
        val = getattr(row, value_col)
        ratio = float(val / max_val) if max_val else 0.0
        label = f"{getattr(row, name_col)} — €{val:.2f}"
        st.markdown(_progress_bar_html(ratio, label), unsafe_allow_html=True)


def _render_category_table(df: pd.DataFrame) -> None:
    _C_SPENT = "Total spent (€)"
    st.subheader("All categories")
    display = df[["category", "subcategory", "total_spent"]].rename(
        columns={"category": "Category", "subcategory": "Subcategory", "total_spent": _C_SPENT}
    )
    st.dataframe(
        display,
        column_config={
            _C_SPENT: st.column_config.ProgressColumn(
                _C_SPENT,
                format="€ %.2f",
                min_value=0,
                max_value=float(df["total_spent"].max() or 1),
            )
        },
        hide_index=True,
        width="stretch",
    )


def page_finances() -> None:
    header_col, filter_col = st.columns([3, 2])
    header_col.header("💶 Finances")
    with filter_col:
        st.write("")
        selected_months = period_filter_pills("finances_period")

    since = months_to_since(selected_months)

    summary = _load_summary(since=since)
    if summary:
        _render_summary_metrics(summary)

    st.divider()

    granularity = granularity_pills("finances_granularity", options=_FINANCES_GRANULARITIES)
    _render_spending_chart(granularity, since=since)

    st.divider()

    cat_df = _load_category_df(since=since)
    if cat_df.empty:
        st.info("No category data yet.")
        return

    cat_totals = cat_df.groupby("category", as_index=False)["total_spent"].sum().sort_values("total_spent", ascending=False)
    subcat_df = cat_df[cat_df["subcategory"] != "Onbekend"].copy()

    col1, col2 = st.columns(2)
    with col1:
        _render_top5(cat_totals, "category", "🏷️ Top categories")
    with col2:
        _render_top5(subcat_df, "subcategory", "🔖 Top subcategories")

    st.divider()

    discount_df = _load_top_discounts_df(since=since)
    if not discount_df.empty:
        _render_top5(discount_df, "name", "💰 Top discounts", value_col="total_discount")
        st.divider()

    _render_category_table(cat_df)
