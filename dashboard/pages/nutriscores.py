import pandas as pd
import streamlit as st

from backend_client import get_nutriscore_distribution, get_nutriscore_products
from chart_config import CHART_COLORS, NUTRISCORE_COLORS
from widgets import months_to_since, period_filter_pills

_SCORE_ORDER = ["A", "B", "C", "D", "E", ""]


def _score_color(score: str) -> str:
    return NUTRISCORE_COLORS.get(score, CHART_COLORS["unknown"])


def _score_label(score: str) -> str:
    return score if score else "Unknown"


def _navigate_to_product(web_id) -> None:
    st.session_state.selected_product_web_id = int(web_id)
    st.switch_page(st.session_state._products_page)


def _render_distribution(df: pd.DataFrame) -> str | None:
    total_bought = df["times_bought"].sum() or 1

    st.subheader("Distribution")
    cols = st.columns(len(df))
    selected = st.session_state.get("_nutriscore_selected")

    for col, row in zip(cols, df.itertuples()):
        score = row.score
        label = _score_label(score)
        color = _score_color(score)
        pct = row.times_bought / total_bought * 100
        is_active = selected == score

        border = f"3px solid {color}" if is_active else "3px solid transparent"
        bg = f"linear-gradient(to top, {color}44 {pct:.0f}%, {color}11 {pct:.0f}%)"
        col.markdown(
            f"""<div style="text-align:center; background:{bg}; border-radius:10px; padding:12px 4px; border:{border};">
              <div style="font-size:2rem; font-weight:900; color:{color};">{label}</div>
              <div style="font-size:1.1rem; font-weight:700;">{pct:.0f}%</div>
              <div style="font-size:0.8rem; opacity:0.7;">{row.times_bought}× bought</div>
              <div style="font-size:0.75rem; opacity:0.6;">{row.count} products</div>
            </div>""",
            unsafe_allow_html=True,
        )
        if col.button(_score_label(score), key=f"ns_btn_{score or 'unknown'}", width="stretch"):
            if selected == score:
                del st.session_state["_nutriscore_selected"]
            else:
                st.session_state["_nutriscore_selected"] = score
            st.rerun()

    return st.session_state.get("_nutriscore_selected")


def _render_products(score: str, since: str | None = None) -> None:
    label = _score_label(score)
    color = _score_color(score)
    st.markdown(f"### Products with nutriscore <span style='color:{color}; font-weight:900;'>{label}</span>", unsafe_allow_html=True)

    try:
        data = get_nutriscore_products(score, since=since)
    except Exception as e:
        st.error(f"Could not load nutriscore products: {e}")
        return
    if not data:
        st.info(f"No purchased products with nutriscore {label}.")
        return

    df = pd.DataFrame(data)
    df = df.sort_values("times_bought", ascending=False).reset_index(drop=True)

    _C_BOUGHT = "Times bought"
    _C_SPENT = "Total spent"
    _C_KG = "Total kg"
    _C_CO2 = "CO₂eq (kg)"

    display = df[["thumbnail_url", "title", "times_bought", "total_spent", "total_kg", "co2eq_total"]].rename(
        columns={
            "thumbnail_url": "Image",
            "title": "Product",
            "times_bought": _C_BOUGHT,
            "total_spent": _C_SPENT,
            "total_kg": _C_KG,
            "co2eq_total": _C_CO2,
        }
    )

    event = st.dataframe(
        display,
        column_config={
            "Image": st.column_config.ImageColumn("", width="small"),
            _C_BOUGHT: st.column_config.ProgressColumn(
                _C_BOUGHT, format="%d×", min_value=0, max_value=int(df["times_bought"].max() or 1)
            ),
            _C_SPENT: st.column_config.ProgressColumn(
                _C_SPENT, format="€ %.2f", min_value=0, max_value=float(df["total_spent"].max() or 1)
            ),
            _C_KG: st.column_config.ProgressColumn(
                _C_KG, format="%.1f kg", min_value=0, max_value=float(df["total_kg"].max() or 1)
            ),
            _C_CO2: st.column_config.ProgressColumn(
                _C_CO2, format="%.2f kg", min_value=0, max_value=float(df["co2eq_total"].max() or 1)
            ),
        },
        on_select="rerun",
        selection_mode="single-row",
        hide_index=True,
        width="stretch",
    )

    if event.selection.rows:
        web_id = df.iloc[event.selection.rows[0]]["web_id"]
        if pd.notna(web_id):
            _navigate_to_product(web_id)


def page_nutriscores() -> None:
    header_col, filter_col = st.columns([3, 2])
    header_col.header("🥗 Nutriscores")
    with filter_col:
        st.write("")
        selected_months = period_filter_pills("nutriscores_period")

    since = months_to_since(selected_months)

    try:
        data = get_nutriscore_distribution(since=since)
    except Exception as e:
        st.error(f"Could not load nutriscore data: {e}")
        return
    if not data:
        st.info("No purchase data yet.")
        return

    df = pd.DataFrame(data)
    df["score"] = df["score"].fillna("")
    scores_present = set(df["score"])
    df = df.set_index("score").reindex(
        [s for s in _SCORE_ORDER if s in scores_present],
        fill_value=0,
    ).reset_index()

    unknown_bought = int(df.loc[df["score"] == "", "times_bought"].sum())
    df = df[(df["times_bought"] > 0) & (df["score"] != "")]

    st.caption("Select a category to view all purchased products in that score group.")
    if unknown_bought:
        st.caption(f"{unknown_bought} purchase(s) are not shown because the AH API does not provide a nutriscore for those products.")

    selected = _render_distribution(df)

    if selected is not None:
        st.divider()
        _render_products(selected, since=since)
