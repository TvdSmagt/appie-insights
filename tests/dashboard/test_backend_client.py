"""Unit tests for dashboard/backend_client.py."""

import responses as resp

from tests.dashboard.conftest import MOCK_BACKEND

from backend_client import get_product_detail


class TestGetProductDetail:
    @resp.activate
    def test_returns_none_on_not_found(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/products/123", status=404)

        assert get_product_detail(123) is None

    @resp.activate
    def test_returns_none_on_backend_error(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/products/123", status=500)

        assert get_product_detail(123) is None