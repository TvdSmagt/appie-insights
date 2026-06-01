"""Fixtures shared across dashboard unit tests."""

import pytest

MOCK_BACKEND = "http://mock-backend"


@pytest.fixture(autouse=True)
def _patch_backend_url(monkeypatch):
    """Point backend_client at the mock URL so no real HTTP calls are made."""
    import backend_client
    monkeypatch.setattr(backend_client, "BACKEND_URL", MOCK_BACKEND)


@pytest.fixture(autouse=True)
def _silence_st_error(monkeypatch):
    """Suppress st.error — there is no running Streamlit app in unit tests."""
    import streamlit as st
    monkeypatch.setattr(st, "error", lambda *a, **kw: None)
