"""Settings page — theme switcher, maintenance actions, and danger zone."""

import streamlit as st

from backend_client import (
    _get,
    clear_all_enrichment,
    logout,
    reset_database,
    run_enrichment_with_progress,
    trigger_sync,
)
from settings import (
    AE_WEIGHTS,
    calculate_ae,
    get_household_ae,
    set_household_ae,
)
from theme import DARK, LIGHT, get_theme, set_theme


@st.dialog("Re-enrich everything?")
def _reenrich_dialog() -> None:
    n = _get("/enrichment/count").get("count", 0)
    st.warning(
        f"This will delete all **{n}** existing enrichment row(s) "
        "and re-run automatic matching from scratch."
    )
    c1, c2 = st.columns(2)
    if c1.button("Yes, re-enrich all", type="primary"):
        clear_all_enrichment()
        st.session_state.pop("_confirm_reenrich", None)
        run_enrichment_with_progress()
        st.cache_data.clear()
        st.rerun()
    if c2.button("Cancel"):
        st.session_state.pop("_confirm_reenrich", None)
        st.rerun()


@st.dialog("Reset database?")
def _reset_db_dialog() -> None:
    st.error("All data will be **permanently deleted**. There is no way to recover it.")
    confirm = st.text_input("Type **reset** to confirm")
    c1, c2 = st.columns(2)
    if c1.button("Delete everything", type="primary", disabled=confirm != "reset"):
        reset_database()
        st.session_state.pop("_confirm_reset_db", None)
        st.cache_data.clear()
        st.rerun()
    if c2.button("Cancel"):
        st.session_state.pop("_confirm_reset_db", None)
        st.rerun()


def page_settings() -> None:
    st.header("⚙️ Settings")

    # ── Appearance ────────────────────────────────────────────────────────────
    st.subheader("Appearance")

    current = get_theme()
    col_label, col_toggle = st.columns([4, 1])
    col_label.write("**Theme**")
    col_label.caption(
        "Light theme follows the Albert Heijn app style. "
        "Dark theme is the default blue-dark look. "
        "Switching theme reloads the page to apply the new colors."
    )
    toggle_label = "☀️ Light" if current == DARK else "🌙 Dark"
    if col_toggle.button(toggle_label, key="theme_toggle"):
        set_theme(LIGHT if current == DARK else DARK)
        st.components.v1.html("<script>parent.window.location.reload()</script>", height=0)

    st.divider()

    # ── Household ─────────────────────────────────────────────────────────────
    st.subheader("Household")
    st.caption(
        "Used to scale the CO₂ reference lines on the Dashboard. "
        "1 adult-equivalent (AE) = one adult's average food footprint."
    )

    current_ae = get_household_ae()
    if "_apply_household_ae" in st.session_state:
        st.session_state["household_ae_input"] = st.session_state.pop(
            "_apply_household_ae"
        )

    col_label, col_input = st.columns([4, 1])
    col_label.write("**Adult-equivalents**")
    new_ae = col_input.number_input(
        "Adult-equivalents",
        min_value=0.1,
        max_value=20.0,
        value=current_ae,
        step=0.05,
        format="%.2f",
        label_visibility="collapsed",
        key="household_ae_input",
    )
    if round(new_ae, 2) != round(current_ae, 2):
        set_household_ae(round(new_ae, 2))
        st.rerun()

    with st.expander("Calculator — estimate from household composition"):
        st.caption(
            "Enter the number of people in each age group to compute adult-equivalents."
        )
        counts: dict[str, int] = {}
        for label in AE_WEIGHTS:
            counts[label] = st.number_input(
                f"{label} (×{AE_WEIGHTS[label]})",
                min_value=0,
                max_value=20,
                value=0,
                step=1,
                key=f"ae_calc_{label}",
            )
        estimated = calculate_ae(counts)
        st.metric("Estimated adult-equivalents", f"{estimated:.2f}")
        if st.button("Apply this value", disabled=estimated == 0):
            value = round(estimated, 2)
            set_household_ae(value)
            st.session_state["_apply_household_ae"] = value
            st.rerun()

    st.divider()

    # ── Maintenance ───────────────────────────────────────────────────────────
    st.subheader("Maintenance")
    st.caption(
        "These actions trigger background workers. Progress is shown in the sidebar while they run."
    )

    col_sync, col_enrich = st.columns(2)
    if col_sync.button("🔄 Synchronize", width="stretch", type="secondary"):
        try:
            trigger_sync()
            st.toast("Sync started")
        except Exception as exc:
            st.error(f"Could not start sync: {exc}")
    if col_enrich.button("♻️ Re-enrich everything", width="stretch", type="secondary"):
        st.session_state["_confirm_reenrich"] = True
    if st.session_state.get("_confirm_reenrich"):
        _reenrich_dialog()

    st.divider()

    # ── Account ───────────────────────────────────────────────────────────────
    st.subheader("Account")

    st.caption(
        "This app supports a single account. If you log in with a different Albert Heijn "
        "account after logging out, the new data will be merged into the existing database. "
        "Reset the database first if you want to start fresh."
    )
    if st.button("🚪 Logout", width="stretch", type="secondary"):
        logout()
        st.cache_data.clear()
        st.rerun()

    st.divider()

    # ── Danger zone ───────────────────────────────────────────────────────────
    st.subheader("Danger zone")

    with st.expander("🗑️ Reset database"):
        st.error(
            "Permanently deletes **all data** — receipts, orders, enrichment, "
            "and everything else. This **cannot be undone**."
        )
        if st.button("Reset database", type="primary", key="reset_db_btn"):
            st.session_state["_confirm_reset_db"] = True
    if st.session_state.get("_confirm_reset_db"):
        _reset_db_dialog()
