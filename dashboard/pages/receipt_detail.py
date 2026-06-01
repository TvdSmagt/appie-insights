import pandas as pd
import streamlit as st

from loaders import load_receipt_items
from backend_client import get_receipt_detail, run_enrichment_with_progress


def receipt_detail() -> None:
    receipt_id = st.session_state.get("selected_receipt_id")
    if receipt_id is None:
        st.info("Select a receipt from the Purchases page.")
        return

    col_back, col_title = st.columns([1, 9])
    with col_back:
        if st.button("← Back"):
            del st.session_state["selected_receipt_id"]
            st.rerun()

    detail = get_receipt_detail(receipt_id)
    if detail:
        date = pd.to_datetime(detail["date"], errors="coerce")
        with col_title:
            st.subheader(f"{date.strftime('%-d %B %Y')} — € {detail['total_amount']:.2f}")

    items = load_receipt_items(receipt_id)

    c1, c2, c3, c4 = st.columns(4)
    c1.metric("Items", detail.get("item_count", len(items)) if detail else len(items))
    c2.metric("Matched", detail.get("matched_count", 0) if detail else 0)
    co2_total = detail.get("co2eq_total") if detail else None
    co2_per_euro = detail.get("co2eq_per_euro") if detail else None
    c3.metric("Total CO₂eq", f"{co2_total:.1f} kg" if co2_total is not None else "—")
    c4.metric("CO₂eq / €", f"{co2_per_euro:.3f}" if co2_per_euro is not None else "—")

    if st.button("🔄 Re-enrich this receipt"):
        run_enrichment_with_progress(receipt_id=receipt_id)
        st.cache_data.clear()
        st.rerun()

    items = items.copy()
    items["display_name"] = items["web_title"].replace("", pd.NA).fillna(items["description"])

    receipt_items_event = st.dataframe(
        items[
            [
                "thumbnail_url",
                "display_name",
                "quantity",
                "amount",
                "ah_category",
                "co2eq_name",
                "co2eq_per_kg",
                "co2eq_total",
            ]
        ].rename(
            columns={
                "thumbnail_url": "Image",
                "display_name": "Product",
                "quantity": "Qty",
                "amount": "Total (€)",
                "ah_category": "Category",
                "co2eq_name": "CO₂ Match",
                "co2eq_per_kg": "CO₂/kg",
                "co2eq_total": "CO₂eq total",
            }
        ),
        column_config={
            "Image": st.column_config.ImageColumn("Image", width="small"),
        },
        on_select="rerun",
        selection_mode="single-row",
        width="stretch",
        hide_index=True,
    )
    if receipt_items_event.selection.rows:
        idx = receipt_items_event.selection.rows[0]
        row = items.iloc[idx]
        wid = row.get("web_id")
        st.session_state._product_nav_source = "receipts"
        try:
            st.session_state.selected_product_web_id = int(wid)
            if "_products_page" in st.session_state:
                st.switch_page(st.session_state._products_page)
        except (TypeError, ValueError):
            pos_id = row.get("pos_id")
            try:
                st.session_state.selected_product_pos_id = int(pos_id)
                if "_products_page" in st.session_state:
                    st.switch_page(st.session_state._products_page)
            except (TypeError, ValueError):
                pass
