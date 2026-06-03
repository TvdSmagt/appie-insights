import pandas as pd
import streamlit as st

from backend_client import get_order_detail
from formatting import format_date


def order_detail() -> None:
    order_id = st.session_state.get("selected_order_id")
    if order_id is None:
        st.info("Select an order from the Purchases page.")
        return

    col_back, col_title = st.columns([1, 9])
    with col_back:
        if st.button("← Back"):
            del st.session_state["selected_order_id"]
            st.rerun()

    detail = get_order_detail(order_id)
    if detail is None:
        st.warning(f"Order {order_id} not found.")
        return

    delivery_date = pd.to_datetime(detail["delivery_date"], errors="coerce")
    with col_title:
        st.subheader(f"{format_date(delivery_date, '%d %B %Y')} — € {detail['total_price']:.2f}")

    address_street = detail.get("address_street", "")
    if address_street:
        st.caption(
            f"{detail['delivery_method']} • {address_street} {detail['address_number']}"
            f"{detail.get('address_extra') or ''}, "
            f"{detail['address_postcode']} {detail['address_city']}"
        )

    items = pd.DataFrame(detail.get("items", []))

    if items.empty:
        st.info("No items for this order.")
        return

    for col in ["co2eq_per_kg", "weight_per_unit_kg", "co2eq_total", "unit_price", "allocated_qty"]:
        if col in items.columns:
            items[col] = pd.to_numeric(items[col], errors="coerce")

    matched = items[items["co2eq_per_kg"].notna()]
    co2_total = detail.get("co2eq_total")
    co2_per_euro = detail.get("co2eq_per_euro")

    c1, c2, c3, c4 = st.columns(4)
    c1.metric("Items", len(items))
    c2.metric("Matched", len(matched))
    c3.metric("Total CO₂eq", f"{co2_total:.1f} kg" if co2_total is not None else "—")
    c4.metric("CO₂eq / €", f"{co2_per_euro:.3f}" if co2_per_euro is not None else "—")

    cols = ["image_url", "title", "allocated_qty", "brand", "category", "sales_unit_size", "line_total"]
    rename = {
        "image_url": "Image",
        "title": "Product",
        "allocated_qty": "Qty",
        "brand": "Brand",
        "category": "Category",
        "sales_unit_size": "Size",
        "line_total": "Total (€)",
    }
    col_config: dict = {
        "Image": st.column_config.ImageColumn("Image", width="small"),
        "Total (€)": st.column_config.NumberColumn(format="%.2f"),
    }

    for field, label in [("weight_per_unit_kg", "Weight (kg)"), ("co2eq_per_kg", "CO₂/kg"), ("co2eq_total", "CO₂eq total")]:
        if field in items.columns:
            cols.append(field)
            rename[field] = label

    order_items_event = st.dataframe(
        items[cols].rename(columns=rename),
        column_config=col_config,
        on_select="rerun",
        selection_mode="single-row",
        width="stretch",
        hide_index=True,
    )
    if order_items_event.selection.rows:
        idx = order_items_event.selection.rows[0]
        wid = items.iloc[idx].get("web_id")
        try:
            st.session_state.selected_product_web_id = int(wid)
            st.session_state._product_nav_source = "orders"
            if "_products_page" in st.session_state:
                st.switch_page(st.session_state._products_page)
        except (TypeError, ValueError):
            pass
