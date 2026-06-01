import pandas as pd
import streamlit as st

from backend_client import search_all


def page_search() -> None:
    st.header("🔍 Search")

    query = st.text_input("Search", placeholder="Product name, description, category...")
    if not query or len(query) < 2:
        st.caption("Enter at least 2 characters to search.")
        return

    try:
        results = search_all(query)
    except Exception as e:
        st.error(f"Could not reach backend: {e}")
        return

    products = pd.DataFrame(results.get("products", []))
    receipt_items = pd.DataFrame(results.get("receipt_items", []))
    order_items = pd.DataFrame(results.get("order_items", []))

    tab_products, tab_receipts, tab_orders = st.tabs(
        [
            f"Products ({len(products)})",
            f"Receipt Items ({len(receipt_items)})",
            f"Order Items ({len(order_items)})",
        ]
    )

    with tab_products:
        if products.empty:
            st.info("No products found.")
        else:
            event = st.dataframe(
                products.rename(
                    columns={
                        "thumbnail_url": "Image",
                        "title": "Title",
                        "brand": "Brand",
                        "ah_category": "Category",
                        "unit_size": "Unit size",
                    }
                ),
                column_config={
                    "Image": st.column_config.ImageColumn("Image", width="small"),
                    "web_id": None,
                },
                on_select="rerun",
                selection_mode="single-row",
                hide_index=True,
                width="stretch",
                key="search_products",
            )
            if event.selection.rows:
                idx = event.selection.rows[0]
                wid = products.iloc[idx]["web_id"]
                if pd.notna(wid):
                    st.session_state.selected_product_web_id = int(wid)
                    st.switch_page(st.session_state._products_page)

    with tab_receipts:
        if receipt_items.empty:
            st.info("No receipt items found.")
        else:
            receipt_items["date"] = pd.to_datetime(receipt_items["date"], errors="coerce").dt.strftime("%-d %b %Y")
            st.dataframe(
                receipt_items.rename(
                    columns={
                        "date": "Date",
                        "transaction_id": "Receipt",
                        "description": "Product",
                        "quantity": "Qty",
                        "amount": "€",
                    }
                ),
                column_config={"€": st.column_config.NumberColumn(format="€ %.2f")},
                hide_index=True,
                width="stretch",
            )

    with tab_orders:
        if order_items.empty:
            st.info("No order items found.")
        else:
            order_items["delivery_date"] = pd.to_datetime(order_items["delivery_date"], errors="coerce").dt.strftime("%-d %b %Y")
            st.dataframe(
                order_items.rename(
                    columns={
                        "delivery_date": "Date",
                        "order_id": "Order",
                        "title": "Product",
                        "brand": "Brand",
                        "quantity": "Qty",
                        "amount": "€",
                    }
                ),
                column_config={"€": st.column_config.NumberColumn(format="€ %.2f")},
                hide_index=True,
                width="stretch",
            )
