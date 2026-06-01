"""Interactive chart components for the dashboard."""

import pandas as pd
import plotly.express as px
import plotly.graph_objects as go
import streamlit as st

from chart_config import CATEGORY_COLORS, CATEGORY_ICONS
from theme import get_plotly_template


def create_interactive_trend_chart(
    trend_df: pd.DataFrame,
    show_avg: bool = True,
    avg_line_val: float = 0.0,
    show_sustainable: bool = True,
    sus_line_val: float = 0.0,
    show_ma: bool = False,
    ma_window: int = 3,
) -> go.Figure:
    """Create a stacked bar chart showing CO₂ emissions by period and category.

    Args:
        trend_df: DataFrame with columns: period, category, co2eq (pre-aggregated from backend)
    """
    pivot = trend_df.pivot(index="period", columns="category", values="co2eq").fillna(0)

    fig = go.Figure()
    for cat_name in pivot.columns:
        fig.add_trace(
            go.Bar(
                x=pivot.index,
                y=pivot[cat_name],
                name=cat_name,
                marker_color=CATEGORY_COLORS.get(cat_name, "#AAAAAA"),
                customdata=pivot.index,
                hovertemplate=f"<b>{cat_name}</b><br>Period: %{{customdata}}<br>CO₂eq: %{{y:.1f}} kg<br><i>Click to see products</i><extra></extra>",
            )
        )

    if show_avg:
        fig.add_hline(
            y=avg_line_val,
            line_dash="dash",
            line_color="#FFA726",
            line_width=2,
            annotation_text=f"Dutch avg — {avg_line_val:.0f} kg",
            annotation_position="top left",
            annotation_font_color="#FFA726",
        )

    if show_sustainable:
        fig.add_hline(
            y=sus_line_val,
            line_dash="dot",
            line_color="#66BB6A",
            line_width=2,
            annotation_text=f"Sustainable target — {sus_line_val:.0f} kg",
            annotation_position="top left",
            annotation_font_color="#66BB6A",
        )

    if show_ma and not trend_df.empty:
        totals = (
            trend_df.groupby("period")["co2eq"]
            .sum()
            .reset_index()
            .sort_values("period")
        )
        totals["ma"] = totals["co2eq"].rolling(window=ma_window, min_periods=1).mean()
        fig.add_trace(
            go.Scatter(
                x=totals["period"],
                y=totals["ma"],
                name=f"{ma_window}-period MA",
                mode="lines+markers",
                line={"color": "#CE93D8", "width": 2, "dash": "solid"},
                marker={"size": 5},
                hovertemplate=f"<b>{ma_window}-period MA</b><br>Period: %{{x}}<br>CO₂eq: %{{y:.1f}} kg<extra></extra>",
            )
        )

    fig.update_layout(
        title="Total CO₂eq per period — Select a period above to see products by category",
        xaxis_title="Period",
        yaxis_title="kg CO₂eq",
        barmode="stack",
        template=get_plotly_template(),
        hovermode="closest",
    )

    return fig


def render_interactive_trend_chart(
    trend_df: pd.DataFrame,
    show_avg: bool = True,
    avg_line_val: float = 0.0,
    show_sustainable: bool = True,
    sus_line_val: float = 0.0,
    show_ma: bool = False,
    ma_window: int = 3,
) -> tuple[str | None, str | None]:
    """Render the trend chart with reference lines.

    Returns the (period, category) of a clicked bar, or (None, None) if nothing was clicked.
    """
    pivot = trend_df.pivot(index="period", columns="category", values="co2eq").fillna(0)
    category_names = list(pivot.columns)
    period_names = list(pivot.index)

    fig = create_interactive_trend_chart(
        trend_df=trend_df,
        show_avg=show_avg,
        avg_line_val=avg_line_val,
        show_sustainable=show_sustainable,
        sus_line_val=sus_line_val,
        show_ma=show_ma,
        ma_window=ma_window,
    )

    event = st.plotly_chart(
        fig,
        width="stretch",
        key="trend_chart_main",
        on_select="rerun",
        selection_mode=["points"],
    )

    points = (event.selection or {}).get("points", []) if event else []
    if points:
        point = points[0]
        point_number = point.get("point_number", 0)
        curve_number = point.get("curve_number", 0)
        clicked_period = (
            period_names[point_number] if point_number < len(period_names) else None
        )
        clicked_category = (
            category_names[curve_number] if curve_number < len(category_names) else None
        )
        return clicked_period, clicked_category

    return None, None


def get_category_icon(category: str) -> str:
    """Return the emoji icon for a category."""
    return CATEGORY_ICONS.get(category, "📊")


def _render_product_table(products: pd.DataFrame) -> tuple[bool, int | None]:
    """Render a product table and return (row_selected, web_id)."""
    display_df = products.copy()
    display_df["Qty"] = display_df["quantity"].apply(
        lambda x: f"{x:.1f}" if pd.notna(x) else "—"
    )
    display_df["Weight (kg)"] = display_df["weight_per_unit_kg"].apply(
        lambda x: f"{x:.3f}" if pd.notna(x) and x > 0 else "—"
    )
    display_df["CO₂/kg"] = display_df["co2eq_per_kg"].apply(
        lambda x: f"{x:.2f}" if pd.notna(x) else "—"
    )
    display_df["CO₂ (kg)"] = display_df["co2eq_total"].apply(
        lambda x: f"{x:.3f}" if pd.notna(x) else "—"
    )
    display_df["% of Category"] = display_df["percentage_of_category"].apply(
        lambda x: f"{x:.1f}%"
    )
    display_df["Matched As"] = display_df["co2eq_name"].fillna("—")
    display_df["Web Title"] = display_df["web_title"].fillna("—")

    st.caption("Click a row to open the product page.")
    event = st.dataframe(
        display_df[
            ["description", "Web Title", "Matched As", "Qty", "Weight (kg)", "CO₂/kg", "CO₂ (kg)", "% of Category"]
        ].rename(columns={"description": "Product"}),
        on_select="rerun",
        selection_mode="single-row",
        width="stretch",
        hide_index=True,
        column_config={
            "Product": st.column_config.TextColumn(width="large"),
            "Web Title": st.column_config.TextColumn(width="large"),
            "Matched As": st.column_config.TextColumn(width="medium"),
            "Qty": st.column_config.TextColumn(width="small"),
            "Weight (kg)": st.column_config.TextColumn(width="small"),
            "CO₂/kg": st.column_config.TextColumn(width="small"),
            "CO₂ (kg)": st.column_config.TextColumn(width="small"),
            "% of Category": st.column_config.TextColumn(width="small"),
        },
    )

    if event.selection.rows:
        idx = event.selection.rows[0]
        wid = products.iloc[idx].get("web_id")
        try:
            return True, int(wid)
        except (TypeError, ValueError):
            return True, None
    return False, None


def _render_category_body(products: pd.DataFrame) -> None:
    """Shared display logic for category drilldown: metrics + table + insights."""
    if products.empty:
        return

    total_co2 = products["co2eq_total"].sum()
    num_items = len(products)
    total_spent = products["amount"].sum()

    col1, col2, col3, col4 = st.columns(4)
    col1.metric("Total CO₂", f"{total_co2:.2f} kg")
    col2.metric("Items", num_items)
    col3.metric("Avg per Item", f"{total_co2 / num_items:.2f} kg")
    col4.metric("Total Spent", f"€ {total_spent:.2f}")

    st.markdown("**Sorted by CO₂ from high to low:**")
    selected, clicked_web_id = _render_product_table(products)
    if selected:
        if clicked_web_id is not None:
            st.session_state.selected_product_web_id = clicked_web_id
            if "_products_page" in st.session_state:
                st.switch_page(st.session_state._products_page)
        else:
            st.info("No product detail page available for this item — it has no web ID mapping yet. Use the Data Quality page to link it.")

    st.markdown("---")
    col1, col2 = st.columns(2)
    top = products.iloc[0]
    with col1:
        st.success(
            f"**🔴 Biggest impact**: {top['description']}\n\n"
            f"{top['co2eq_total']:.3f} kg CO₂ ({top['percentage_of_category']:.1f}%)"
        )
    with col2:
        low = products.iloc[-1]
        st.info(
            f"**🟢 Lowest impact**: {low['description']}\n\n"
            f"{low['co2eq_total']:.3f} kg CO₂ ({low['percentage_of_category']:.1f}%)"
        )


def render_category_details_modal(
    products_df: pd.DataFrame, period: str, category: str
) -> None:
    """Show all products in a category for a specific period."""
    icon = CATEGORY_ICONS.get(category, "📊")
    st.subheader(f"{icon} {category} in {period}")

    _, col_back = st.columns([5, 1])
    with col_back:
        if st.button("← Back", key="back_to_categories"):
            st.session_state.pop("_drill_category", None)
            st.rerun()

    if products_df.empty:
        st.info(f"No {category} products found for {period}.")
        return

    _render_category_body(products_df)


def render_category_alltime_details(products_df: pd.DataFrame, category: str) -> None:
    """Show all products in a category across all time (no period filter)."""
    icon = CATEGORY_ICONS.get(category, "📊")
    st.subheader(f"{icon} {category} — all time")

    if products_df.empty:
        st.info(f"No {category} products found.")
        return

    _render_category_body(products_df)


def render_category_breakdown_chart(
    cats_df: pd.DataFrame,
) -> str | None:
    """Render pie + bar charts for all-time category breakdown.

    Args:
        cats_df: DataFrame with columns: category, co2eq (pre-aggregated from backend)

    Returns the name of the clicked category, or None.
    """
    col_a, col_b = st.columns(2)
    clicked: str | None = None

    with col_a:
        pie = px.pie(
            cats_df,
            values="co2eq",
            names="category",
            title="CO₂eq by Category — click to drill down",
            color_discrete_sequence=list(CATEGORY_COLORS.values()),
            template=get_plotly_template(),
        )
        pie_event = st.plotly_chart(
            pie,
            width="stretch",
            key="cat_pie",
            on_select="rerun",
            selection_mode=["points"],
        )
        pts = (pie_event.selection or {}).get("points", []) if pie_event else []
        if pts:
            clicked = pts[0].get("label")

    with col_b:
        bar = px.bar(
            cats_df.sort_values("co2eq", ascending=True),
            x="co2eq",
            y="category",
            orientation="h",
            labels={"category": "Category", "co2eq": "kg CO₂eq"},
            title="CO₂eq by Category — click to drill down",
            color_discrete_sequence=["#0072CE"],
            template=get_plotly_template(),
        )
        bar_event = st.plotly_chart(
            bar,
            width="stretch",
            key="cat_bar",
            on_select="rerun",
            selection_mode=["points"],
        )
        pts = (bar_event.selection or {}).get("points", []) if bar_event else []
        if pts and clicked is None:
            clicked = pts[0].get("y")

    return clicked
