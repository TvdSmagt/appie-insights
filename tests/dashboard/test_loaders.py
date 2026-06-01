"""Unit tests for dashboard/loaders.py with a mock HTTP backend."""

import pandas as pd
import pytest
import responses as resp

from tests.dashboard.conftest import MOCK_BACKEND
from loaders import (
    load_combined_enriched_items,
    load_enriched_items,
    load_products,
    load_receipt_items,
    load_receipts,
)

# --- Fixture data matching the API spec ---

_RECEIPTS = [
    {
        "transaction_id": "TRX-001",
        "date": "2026-01-15T10:00:00Z",
        "total_amount": 34.50,
        "item_count": 3,
        "matched_count": 2,
        "co2eq_total": 2.1,
    }
]

_ITEMS = [
    {
        "source_type": "receipt",
        "description": "Melk",
        "quantity": 2,
        "amount": 3.00,
        "date": "2026-01-15T10:00:00Z",
        "co2eq_category": "Zuivel",
        "co2eq_name": "Melk",
        "co2eq_per_kg": 3.2,
        "match_method": "exact",
        "weight_kg": 1.0,
        "unit_size": "1 liter",
        "web_id": 456,
        "web_title": "Melk Halfvol",
        "weight_per_unit_kg": 1.0,
        "co2eq_total": 6.4,
    },
    {
        "source_type": "order",
        "description": "Kaas",
        "quantity": 1,
        "amount": 4.50,
        "date": "2026-01-20T09:00:00Z",
        "co2eq_category": "Zuivel",
        "co2eq_name": "Kaas",
        "co2eq_per_kg": 8.5,
        "match_method": "fuzzy",
        "weight_kg": None,
        "unit_size": "500g",
        "web_id": 789,
        "web_title": "Gouda Kaas",
        "weight_per_unit_kg": 0.5,
        "co2eq_total": 4.25,
    },
]

_RECEIPT_DETAIL = {
    "transaction_id": "TRX-001",
    "date": "2026-01-15T10:00:00Z",
    "total_amount": 34.50,
    "item_count": 3,
    "matched_count": 2,
    "co2eq_total": 2.1,
    "items": [
        {
            "id": 1,
            "description": "Melk",
            "quantity": 2,
            "amount": 3.00,
            "web_id": 456,
            "pos_id": None,
            "co2eq_category": "Zuivel",
            "co2eq_name": "Melk",
            "co2eq_per_kg": 3.2,
            "match_method": "exact",
            "match_score": 100,
            "weight_kg": 1.0,
            "unit_size": "1L",
            "thumbnail_url": None,
            "weight_per_unit_kg": 1.0,
            "co2eq_total": 6.4,
        }
    ],
}

_PRODUCTS = [
    {
        "web_id": 456,
        "thumbnail_url": None,
        "title": "Melk Halfvol",
        "brand": "AH",
        "ah_category": "Zuivel",
        "ah_subcategory": "Melk",
        "unit_size": "1 liter",
        "nutriscore": "B",
        "unit_price_description": "€1.50/l",
        "property_icons": None,
        "co2eq_per_kg": 3.2,
        "co2eq_category": "Zuivel",
    }
]


# --- load_receipts ---

class TestLoadReceipts:
    @resp.activate
    def test_returns_dataframe_with_required_columns(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts", json=_RECEIPTS)
        df = load_receipts()
        assert not df.empty
        assert {"transaction_id", "date", "total_amount", "item_count", "matched_count", "co2eq_total"}.issubset(df.columns)

    @resp.activate
    def test_date_column_is_utc_datetime(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts", json=_RECEIPTS)
        df = load_receipts()
        assert pd.api.types.is_datetime64_any_dtype(df["date"])
        assert str(df["date"].dt.tz) == "UTC"

    @resp.activate
    def test_date_column_comparable_with_utc_timestamp(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts", json=_RECEIPTS)
        df = load_receipts()
        cutoff = pd.Timestamp("2026-01-01", tz="UTC")
        filtered = df[df["date"] >= cutoff]
        assert len(filtered) == 1

    @resp.activate
    def test_empty_response_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts", json=[])
        df = load_receipts()
        assert df.empty

    @resp.activate
    def test_backend_unreachable_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts", body=Exception("unreachable"))
        df = load_receipts()
        assert df.empty


# --- load_combined_enriched_items ---

class TestLoadCombinedEnrichedItems:
    @resp.activate
    def test_returns_dataframe_with_required_columns(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_combined_enriched_items()
        assert not df.empty
        assert {"source_type", "description", "quantity", "amount", "date"}.issubset(df.columns)

    @resp.activate
    def test_contains_both_source_types(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_combined_enriched_items()
        assert set(df["source_type"].unique()) == {"receipt", "order"}

    @resp.activate
    def test_date_column_is_utc_datetime(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_combined_enriched_items()
        assert pd.api.types.is_datetime64_any_dtype(df["date"])
        assert str(df["date"].dt.tz) == "UTC"

    @resp.activate
    def test_date_column_comparable_with_utc_timestamp(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_combined_enriched_items()
        cutoff = pd.Timestamp("2026-01-01", tz="UTC")
        filtered = df[df["date"] >= cutoff]
        assert len(filtered) == 2

    @resp.activate
    def test_numeric_columns_coerced(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_combined_enriched_items()
        for col in ["quantity", "amount", "co2eq_per_kg"]:
            assert pd.api.types.is_numeric_dtype(df[col]), f"{col} should be numeric"

    @resp.activate
    def test_backend_unreachable_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", body=Exception("unreachable"))
        df = load_combined_enriched_items()
        assert df.empty


# --- load_enriched_items ---

class TestLoadEnrichedItems:
    @resp.activate
    def test_returns_only_receipt_items(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/items", json=_ITEMS)
        df = load_enriched_items()
        assert not df.empty
        assert (df["source_type"] == "receipt").all()


# --- load_receipt_items ---

class TestLoadReceiptItems:
    @resp.activate
    def test_returns_items_for_receipt(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts/TRX-001", json=_RECEIPT_DETAIL)
        df = load_receipt_items("TRX-001")
        assert not df.empty
        assert {"description", "quantity", "amount"}.issubset(df.columns)

    @resp.activate
    def test_not_found_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts/TRX-999", status=404)
        df = load_receipt_items("TRX-999")
        assert df.empty

    @resp.activate
    def test_backend_unreachable_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts/TRX-001", body=Exception("unreachable"))
        df = load_receipt_items("TRX-001")
        assert df.empty

    @resp.activate
    def test_numeric_columns_coerced(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/receipts/TRX-001", json=_RECEIPT_DETAIL)
        df = load_receipt_items("TRX-001")
        for col in ["quantity", "amount", "co2eq_per_kg"]:
            assert pd.api.types.is_numeric_dtype(df[col]), f"{col} should be numeric"


# --- load_products ---

class TestLoadProducts:
    @resp.activate
    def test_returns_dataframe_with_required_columns(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/products", json=_PRODUCTS)
        df = load_products()
        assert not df.empty
        assert {"web_id", "title", "co2eq_per_kg"}.issubset(df.columns)

    @resp.activate
    def test_empty_response_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/products", json=[])
        df = load_products()
        assert df.empty

    @resp.activate
    def test_backend_unreachable_returns_empty_dataframe(self):
        resp.add(resp.GET, f"{MOCK_BACKEND}/products", body=Exception("unreachable"))
        df = load_products()
        assert df.empty
