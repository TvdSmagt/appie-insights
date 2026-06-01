import pandas as pd
import streamlit as st

from backend_client import get_product_stats
from theme import progress_bar_colors
from widgets import months_to_since, period_filter_pills


def _load_product_stats(since: str | None = None) -> pd.DataFrame:
    try:
        data = get_product_stats(since=since)
    except Exception as e:
        st.error(f"Could not load product stats: {e}")
        return pd.DataFrame()
    if not data:
        return pd.DataFrame()
    return pd.DataFrame(data)



def _medal(rank: int) -> str:
    return {1: "🥇", 2: "🥈", 3: "🥉"}.get(rank, f"{rank}.")


def _secondary_label(row, exclude: str) -> str:
    parts = []
    if exclude != "times_bought":
        parts.append(f"{int(row.times_bought)}×")
    if exclude != "total_spent":
        parts.append(f"€{row.total_spent:.2f}")
    if exclude != "total_kg":
        parts.append(f"{row.total_kg:.1f} kg")
    co2 = getattr(row, "co2eq_total", None)
    if exclude != "co2eq_total" and co2 is not None and not pd.isna(co2):
        parts.append(f"{co2:.2f} kg CO₂")
    return "  ·  ".join(parts)


def _progress_bar_html(ratio: float, main_label: str, secondary_label: str) -> str:
    pct = ratio * 100
    c = progress_bar_colors()
    return f"""
<div style="position:relative; background:{c['track']}; border-radius:6px; height:28px; margin:4px 0; overflow:hidden;">
  <div style="background:{c['fill']}; width:{pct:.1f}%; height:100%;"></div>
  <div style="position:absolute; inset:0; display:flex; align-items:center; padding:0 10px; gap:10px; font-size:13px; white-space:nowrap; overflow:hidden;">
    <span style="color:{c['text']}; font-weight:700;">{main_label}</span>
    <span style="color:{c['text']}; opacity:0.55;">{secondary_label}</span>
  </div>
</div>"""


def _navigate_to_product(web_id) -> None:
    st.session_state.selected_product_web_id = int(web_id)
    st.switch_page(st.session_state._products_page)


def _render_leaderboard(df: pd.DataFrame, value_col: str, fmt: str) -> None:
    max_val = df[value_col].max()
    for i, row in enumerate(df.itertuples(), start=1):
        val = getattr(row, value_col)
        ratio = float(val / max_val) if max_val else 0.0

        col_medal, col_img, col_info = st.columns([1, 2, 8])
        with col_medal:
            st.markdown(f"### {_medal(i)}")
        with col_img:
            if isinstance(row.thumbnail_url, str) and row.thumbnail_url:
                st.image(row.thumbnail_url, width=48)
        with col_info:
            if st.button(row.title, key=f"lb_{value_col}_{i}", type="tertiary"):
                _navigate_to_product(row.web_id)
            st.markdown(
                _progress_bar_html(ratio, fmt.format(val), _secondary_label(row, value_col)),
                unsafe_allow_html=True,
            )


def _render_all_products(df: pd.DataFrame) -> None:
    st.subheader("All products")

    table = df[["thumbnail_url", "web_id", "title", "times_bought", "total_spent", "total_kg", "co2eq_total"]].copy()
    table = table.sort_values("times_bought", ascending=False).reset_index(drop=True)

    _C_BOUGHT = "Times bought"
    _C_SPENT = "Total spent"
    _C_KG = "Total kg"
    _C_CO2 = "CO₂eq (kg)"

    event = st.dataframe(
        table[["thumbnail_url", "title", "times_bought", "total_spent", "total_kg", "co2eq_total"]].rename(
            columns={
                "thumbnail_url": "Image",
                "title": "Product",
                "times_bought": _C_BOUGHT,
                "total_spent": _C_SPENT,
                "total_kg": _C_KG,
                "co2eq_total": _C_CO2,
            }
        ),
        column_config={
            "Image": st.column_config.ImageColumn("", width="small"),
            _C_BOUGHT: st.column_config.ProgressColumn(
                _C_BOUGHT, format="%d×", min_value=0, max_value=int(table["times_bought"].max() or 1)
            ),
            _C_SPENT: st.column_config.ProgressColumn(
                _C_SPENT, format="€ %.2f", min_value=0, max_value=float(table["total_spent"].max() or 1)
            ),
            _C_KG: st.column_config.ProgressColumn(
                _C_KG, format="%.1f kg", min_value=0, max_value=float(table["total_kg"].max() or 1)
            ),
            _C_CO2: st.column_config.ProgressColumn(
                _C_CO2, format="%.2f kg", min_value=0, max_value=float(table["co2eq_total"].max() or 1)
            ),
        },
        on_select="rerun",
        selection_mode="single-row",
        hide_index=True,
        width="stretch",
    )

    if event.selection.rows:
        web_id = table.iloc[event.selection.rows[0]]["web_id"]
        if pd.notna(web_id):
            _navigate_to_product(web_id)


def page_insights() -> None:
    header_col, filter_col = st.columns([3, 2])
    header_col.header("🏆 Insights")
    with filter_col:
        st.write("")
        selected_months = period_filter_pills("insights_period")

    df = _load_product_stats(since=months_to_since(selected_months))
    if df.empty:
        st.info("No purchase data yet.")
        return

    top_n = 5
    _cols = ["web_id", "title", "thumbnail_url", "times_bought", "total_spent", "total_kg", "co2eq_total"]

    col1, col2, col3 = st.columns(3)

    with col1:
        st.subheader("🛒 Most bought")
        top = df.nlargest(top_n, "times_bought")[_cols].reset_index(drop=True)
        _render_leaderboard(top, "times_bought", "{:.0f}×")

    with col2:
        st.subheader("💶 Most spent")
        top = df.nlargest(top_n, "total_spent")[_cols].reset_index(drop=True)
        _render_leaderboard(top, "total_spent", "€ {:.2f}")

    with col3:
        st.subheader("🌍 Highest CO₂eq")
        co2_df = df.dropna(subset=["co2eq_total"])
        if co2_df.empty:
            st.info("No CO₂ data yet.")
        else:
            top = co2_df.nlargest(top_n, "co2eq_total")[_cols].reset_index(drop=True)
            _render_leaderboard(top, "co2eq_total", "{:.2f} kg CO₂eq")

    st.divider()
    _render_all_products(df)
