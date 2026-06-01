"""AH Sustainability Tracker — Streamlit dashboard."""

import logging
import os
import sys

import streamlit as st

sys.path.insert(0, os.path.dirname(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "pages"))

from backend_client import get_auth_status, get_version, start_login
from theme import init_theme

st.cache_resource(init_theme)()
from pages.data_quality import page_data_quality
from pages.dashboard import page_dashboard
from pages.finances import page_finances
from pages.insights import page_insights
from pages.nutriscores import page_nutriscores
from pages.products import page_products
from pages.purchases import page_purchases
from pages.search import page_search
from pages.settings_page import page_settings
from pages.sustainability import page_sustainability
from widgets import render_pipeline_status

logging.basicConfig(level=logging.INFO)


@st.fragment(run_every=2)
def _login_status_fragment() -> None:
    """Polls auth status every 2s and triggers a full rerun when login completes."""
    auth = get_auth_status()
    if auth is None:
        return
    if auth.get("logged_in") or not auth.get("in_progress"):
        st.rerun()


def _render_login_page(in_progress: bool, login_error: str, login_url: str) -> None:
    _, col, _ = st.columns([1, 2, 1])
    with col:
        st.title("AH Sustainability Tracker")
        st.markdown("Connect your Albert Heijn account to track your CO₂ footprint.")
        st.divider()
        if in_progress:
            if login_url:
                if st.session_state.get("_login_url_opened") != login_url:
                    st.session_state["_login_url_opened"] = login_url
                    st.components.v1.html(
                        f'<script>window.open({login_url!r}, "_blank");</script>',
                        height=0,
                    )
                st.link_button("Open Albert Heijn login", login_url, type="primary", width="stretch")
                st.info("Complete the login in the opened tab, then return here.")
            else:
                st.info("Opening login page…")
        elif login_error:
            st.error(f"Login failed: {login_error}")
        if not in_progress:
            if st.button("Login with Albert Heijn", type="primary"):
                st.session_state.pop("_login_url_opened", None)
                start_login()
                st.rerun()
    if in_progress:
        _login_status_fragment()


def main() -> None:
    st.set_page_config(
        page_title="AH Sustainability Tracker",
        page_icon="🌱",
        layout="wide",
    )

    auth = get_auth_status()
    if auth is None:
        st.info("Connecting to backend service…")
        st.stop()

    if not auth.get("logged_in"):
        _render_login_page(
            in_progress=auth.get("in_progress", False),
            login_error=auth.get("error", ""),
            login_url=auth.get("login_url", ""),
        )
        return

    purchases_pg = st.Page(page_purchases, title="Purchases", icon="\U0001f9fe")
    products_pg = st.Page(page_products, title="Products", icon="\U0001f6d2")
    st.session_state._receipts_page = purchases_pg
    st.session_state._orders_page = purchases_pg
    st.session_state._products_page = products_pg
    pg = st.navigation(
        [
            st.Page(page_dashboard, title="Home", icon="🏠"),
            purchases_pg,
            products_pg,
            st.Page(page_search, title="Search", icon="\U0001f50d"),
            st.Page(page_finances, title="Finances", icon="💶"),
            st.Page(page_nutriscores, title="Nutriscores", icon="🥗"),
            st.Page(page_sustainability, title="Sustainability", icon="🌱"),
            st.Page(page_insights, title="Insights", icon="🏆"),
            st.Page(page_data_quality, title="Data Quality", icon="🧹"),
            st.Page(page_settings, title="Settings", icon="⚙️"),
        ]
    )

    with st.sidebar:
        st.divider()
        render_pipeline_status()
        version = get_version()
        if version:
            st.caption(version)

    pg.run()


if __name__ == "__main__":
    main()
