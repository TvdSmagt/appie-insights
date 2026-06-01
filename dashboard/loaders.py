import pandas as pd
import streamlit as st

from backend_client import get_items, get_products, get_receipt_detail, get_receipts


def load_receipts() -> pd.DataFrame:
    _empty = pd.DataFrame(columns=["transaction_id", "date", "total_amount", "item_count", "matched_count", "co2eq_total", "weight_total", "discount_total"])
    try:
        data = get_receipts()
    except Exception as e:
        st.error(f"Could not load receipts: {e}")
        return _empty
    if not data:
        return _empty
    df = pd.DataFrame(data)
    df["date"] = pd.to_datetime(df["date"], errors="coerce", utc=True)
    return df


def load_combined_enriched_items() -> pd.DataFrame:
    _empty = pd.DataFrame(columns=["source_type", "description", "quantity", "amount", "date",
                                     "co2eq_category", "co2eq_name", "co2eq_per_kg", "match_method",
                                     "weight_kg", "unit_size", "web_id", "web_title",
                                     "weight_per_unit_kg", "co2eq_total"])
    try:
        data = get_items()
    except Exception as e:
        st.error(f"Could not load items: {e}")
        return _empty
    if not data:
        return _empty
    df = pd.DataFrame(data)
    df["date"] = pd.to_datetime(df["date"], errors="coerce", utc=True)
    for col in ["quantity", "amount", "co2eq_per_kg", "weight_per_unit_kg", "co2eq_total"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    return df


def load_receipt_items(receipt_id: str) -> pd.DataFrame:
    _empty = pd.DataFrame(columns=["id", "description", "web_title", "quantity", "amount",
                                     "ah_category", "co2eq_category", "co2eq_name", "co2eq_per_kg",
                                     "match_method", "weight_kg", "unit_size", "web_id",
                                     "thumbnail_url", "weight_per_unit_kg", "co2eq_total"])
    try:
        detail = get_receipt_detail(receipt_id)
    except Exception as e:
        st.error(f"Could not load receipt: {e}")
        return _empty
    if not detail or not detail.get("items"):
        return _empty
    df = pd.DataFrame(detail["items"])
    for col in ["quantity", "amount", "co2eq_per_kg", "weight_per_unit_kg", "co2eq_total"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    return df


def load_enriched_items() -> pd.DataFrame:
    """Receipt items only, with enrichment data. Subset of load_combined_enriched_items."""
    df = load_combined_enriched_items()
    if df.empty:
        return df
    return df[df["source_type"] == "receipt"].reset_index(drop=True)


def load_products() -> pd.DataFrame:
    _empty = pd.DataFrame(columns=["web_id", "thumbnail_url", "title", "brand", "ah_category",
                                     "ah_subcategory", "unit_size", "nutriscore",
                                     "unit_price_description", "property_icons",
                                     "co2eq_per_kg", "co2eq_category", "weight_per_unit_kg"])
    try:
        data = get_products()
    except Exception as e:
        st.error(f"Could not load products: {e}")
        return _empty
    if not data:
        return _empty
    return pd.DataFrame(data)
