import pandas as pd
import streamlit as st

from backend_client import (
    get_category_products,
    get_sustainability_categories,
    get_sustainability_summary,
    get_sustainability_trend,
)
from chart_config import (
    CATEGORY_ICONS,
    NUTRISCORE_COLORS,
    REF_AVG_KG_PER_PERSON_MONTH,
    REF_SUSTAINABLE_KG_PER_PERSON_MONTH,
)
from interactive_chart import (
    render_category_alltime_details,
    render_category_breakdown_chart,
    render_category_details_modal,
    render_interactive_trend_chart,
)
from settings import MA_WINDOW_MAX, get_household_ae, get_ma_window_for, set_ma_window_for
from widgets import granularity_pills, months_to_since, period_filter_pills


def page_sustainability() -> None:
    header_col, filter_col = st.columns([3, 2])
    header_col.header("🌱 Sustainability")
    with filter_col:
        st.write("")
        selected_months = period_filter_pills("sus_period")

    since = months_to_since(selected_months)
    household_ae = get_household_ae()
    period_label = "all time" if selected_months is None else f"last {selected_months} month{'s' if selected_months != 1 else ''}"

    period = granularity_pills("_sus_period")

    trend_data = get_sustainability_trend(period=period.lower(), since=since)
    trend_df = pd.DataFrame(trend_data) if trend_data else pd.DataFrame(columns=["period", "category", "co2eq"])

    if trend_df.empty:
        st.info(
            "No CO₂ data yet — the enrichment worker is processing items in the background. "
            "Check the sidebar for progress."
        )
        return

    sus = get_sustainability_summary(since=since, household_ae=household_ae)

    score_c1, score_c2, score_c3, score_c4 = st.columns(4)
    score_c1.metric(
        "Household",
        f"{household_ae:.1f} AE",
        help="Adult-equivalent household size (configured in Settings)",
    )

    with score_c2:
        grade = sus.get("grade")
        color = NUTRISCORE_COLORS.get(grade, "#888") if grade else "#888"
        if grade:
            st.markdown(
                f"<p style='margin:0; font-size:0.875rem; opacity:0.6;'>"
                f"Grade ({period_label})</p>"
                f'<span style="background:{color}; color:#fff; font-size:1.4rem; font-weight:900; '
                f'border-radius:6px; padding:1px 12px;">{grade}</span>',
                unsafe_allow_html=True,
            )
        else:
            st.metric(f"Grade ({period_label})", "—")

    with score_c3:
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

    with score_c4:
        top_cat = sus.get("top_category")
        if top_cat:
            icon = CATEGORY_ICONS.get(top_cat, "🌍")
            st.metric("Top emission category", f"{icon} {top_cat}", help="Category contributing the most CO₂eq")
        else:
            st.metric("Top emission category", "—")

    st.divider()

    months_map = {"Day": 1 / 30, "Week": 7 / 30, "Month": 1, "Quarter": 3, "Year": 12}
    months_in_period = months_map[period]

    _REF_AVG = "Dutch average"
    _REF_SUS = "Sustainable target"
    _REF_MA = "Moving average"
    ref_defaults = [_REF_AVG, _REF_SUS, _REF_MA]

    pills_col, ma_col = st.columns([3, 1])
    with pills_col:
        ref_lines = st.pills(
            "Show",
            ref_defaults,
            default=ref_defaults,
            selection_mode="multi",
            key="_sus_ref_lines",
            label_visibility="visible",
        )
    with ma_col:
        st.caption(f"{period} moving avg")
        ma_window = get_ma_window_for(period)
        new_ma = st.number_input(
            "MA window",
            min_value=1,
            max_value=MA_WINDOW_MAX.get(period, 12),
            value=ma_window,
            key=f"sus_ma_{period}",
            label_visibility="collapsed",
        )
        if new_ma != ma_window:
            set_ma_window_for(period, int(new_ma))
            ma_window = int(new_ma)

    show_avg = _REF_AVG in (ref_lines or [])
    show_sustainable = _REF_SUS in (ref_lines or [])
    show_ma = _REF_MA in (ref_lines or [])

    avg_line_val = REF_AVG_KG_PER_PERSON_MONTH * household_ae * months_in_period
    sus_line_val = REF_SUSTAINABLE_KG_PER_PERSON_MONTH * household_ae * months_in_period

    tab1, tab2 = st.tabs(["Trend", "Category Breakdown"])

    with tab1:
        st.subheader("📊 Total CO₂eq per period")
        clicked_period, clicked_category = render_interactive_trend_chart(
            trend_df=trend_df,
            show_avg=show_avg,
            avg_line_val=avg_line_val,
            show_sustainable=show_sustainable,
            sus_line_val=sus_line_val,
            show_ma=show_ma,
            ma_window=ma_window,
        )

        available_periods = sorted(trend_df["period"].unique().tolist(), reverse=True)

        if clicked_period and clicked_period in available_periods:
            st.session_state["_sus_drill_period"] = clicked_period

        st.divider()
        st.markdown("### 🔍 Drill down by category")

        drill_col1, drill_col2 = st.columns(2)
        with drill_col1:
            drill_period = st.selectbox(
                "Period", options=available_periods, key="_sus_drill_period"
            )
        with drill_col2:
            period_data = trend_df[trend_df["period"] == drill_period]
            categories = sorted(period_data["category"].dropna().unique().tolist())
            if clicked_category and clicked_category in categories:
                st.session_state["_sus_drill_category"] = clicked_category
            elif (
                st.session_state.get("_sus_drill_category") not in categories and categories
            ):
                st.session_state["_sus_drill_category"] = categories[0]
            drill_category = st.selectbox(
                "Category", options=categories, key="_sus_drill_category"
            )

        if drill_period and drill_category:
            st.divider()
            products_data = get_category_products(
                drill_category, period_type=period.lower(), period_label=drill_period
            )
            products_df = pd.DataFrame(products_data) if products_data else pd.DataFrame()
            render_category_details_modal(products_df, drill_period, drill_category)

    with tab2:
        cats_data = get_sustainability_categories()
        cats_df = pd.DataFrame(cats_data) if cats_data else pd.DataFrame(columns=["category", "co2eq"])
        clicked_cat = render_category_breakdown_chart(cats_df)
        if clicked_cat:
            st.session_state["_sus_drill_category_alltime"] = clicked_cat

        drill_cat_alltime = st.session_state.get("_sus_drill_category_alltime")
        if drill_cat_alltime:
            st.divider()
            col_back, _ = st.columns([1, 7])
            with col_back:
                if st.button("← Overview", key="back_cat_alltime_sus"):
                    del st.session_state["_sus_drill_category_alltime"]
                    st.rerun()
            alltime_products = get_category_products(drill_cat_alltime)
            alltime_df = pd.DataFrame(alltime_products) if alltime_products else pd.DataFrame()
            render_category_alltime_details(alltime_df, drill_cat_alltime)
