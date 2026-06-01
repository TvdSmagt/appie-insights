import pandas as pd
import streamlit as st

from backend_client import get_orders, get_sync_status
from loaders import load_receipts
from pages.order_detail import order_detail
from pages.receipt_detail import receipt_detail
from widgets import render_cost_co2_chart, render_purchase_table

_HISTORY_OPTIONS: dict[str, int | None] = {
    "All": None,
    "1Y": 365,
    "6M": 180,
    "3M": 90,
    "1M": 30,
}


def page_purchases() -> None:
    st.header("🛍️ Purchases")
    if "selected_receipt_id" in st.session_state:
        receipt_detail()
    elif "selected_order_id" in st.session_state:
        order_detail()
    else:
        _purchase_list()


def _load_orders() -> pd.DataFrame:
    data = get_orders()
    if not data:
        return pd.DataFrame(columns=["order_id", "delivery_date", "delivery_method",
                                      "item_count", "matched_count", "co2eq_total", "total_price",
                                      "weight_total", "discount_total"])
    df = pd.DataFrame(data)
    df["delivery_date"] = pd.to_datetime(df["delivery_date"], errors="coerce")
    df = df.rename(columns={"total_price": "total_price"})
    return df


def _order_type(method: str | None) -> str:
    if method and ("pick" in method.lower() or "ophalen" in method.lower()):
        return "🚗 Pick-up"
    return "🚚 Home Delivery"


def _purchase_list() -> None:
    sync_status = get_sync_status()
    receipts = load_receipts()
    orders = _load_orders()

    if sync_status and sync_status.get("status") == "running":
        st.warning("Resync in progress. Purchases may still be changing and charts can be incomplete.")

    if receipts.empty and orders.empty:
        if sync_status and sync_status.get("status") == "running":
            st.info("Purchases are being rebuilt during the current resync.")
        else:
            st.info("No purchases synced yet.")
        return

    parts = []

    if not receipts.empty:
        receipts["matched_pct"] = (
            receipts["matched_count"] / receipts["item_count"].clip(lower=1) * 100
        ).round(0).astype(int).astype(str) + "%"
        receipts["date_fmt"] = pd.to_datetime(receipts["date"], errors="coerce").dt.strftime("%-d %b %Y")
        r = receipts.rename(columns={"transaction_id": "id", "total_amount": "total", "co2eq_total": "co2eq",
                                      "weight_total": "weight", "discount_total": "discount"})
        r["type"] = "🏪 In-Store"
        r["date"] = r["date"].dt.tz_localize(None) if r["date"].dt.tz is not None else r["date"]
        for col in ["weight", "discount"]:
            if col not in r.columns:
                r[col] = None
        parts.append(r[["id", "date", "date_fmt", "type", "total", "co2eq", "weight", "discount", "item_count", "matched_pct"]])

    if not orders.empty:
        orders["matched_pct"] = (
            orders["matched_count"] / orders["item_count"].clip(lower=1) * 100
        ).round(0).astype(int).astype(str) + "%"
        orders["date_fmt"] = orders["delivery_date"].dt.strftime("%-d %b %Y")
        o = orders.rename(columns={"order_id": "id", "delivery_date": "date", "total_price": "total", "co2eq_total": "co2eq",
                                    "weight_total": "weight", "discount_total": "discount"})
        o["type"] = orders["delivery_method"].apply(_order_type)
        o["date"] = o["date"].dt.tz_localize(None) if o["date"].dt.tz is not None else o["date"]
        for col in ["weight", "discount"]:
            if col not in o.columns:
                o[col] = None
        parts.append(o[["id", "date", "date_fmt", "type", "total", "co2eq", "weight", "discount", "item_count", "matched_pct"]])

    df = pd.concat(parts, ignore_index=True).sort_values("date", ascending=False).reset_index(drop=True)

    selected = render_purchase_table(df, f"{len(df)} purchases — click a row to open", "purchases_list")
    if selected is not None:
        match = df[df["id"] == selected]
        if not match.empty and match.iloc[0]["type"] == "🏪 In-Store":
            st.session_state.selected_receipt_id = selected
        else:
            st.session_state.selected_order_id = int(selected)
        st.rerun()

    history = st.segmented_control(
        "History",
        list(_HISTORY_OPTIONS.keys()),
        default="All",
        label_visibility="collapsed",
        key="purchases_history",
    )
    cutoff_days = _HISTORY_OPTIONS.get(history or "All")
    chart_df = df
    if cutoff_days is not None:
        cutoff = pd.Timestamp.now() - pd.Timedelta(days=cutoff_days)
        chart_df = df[df["date"] >= cutoff]

    render_cost_co2_chart(chart_df)
