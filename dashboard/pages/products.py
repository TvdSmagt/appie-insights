import pandas as pd
import plotly.express as px
import streamlit as st

from loaders import load_products
import re

from backend_client import (
    clear_product_enrichment,
    fetch_product_data,
    get_co2eq_categories,
    get_co2eq_entry,
    get_product_by_pos_id,
    get_product_detail,
    get_product_purchases,
    link_product_pos_id,
    run_enrichment_with_progress,
    save_manual_correction,
)
from chart_config import CHART_COLORS, NUTRISCORE_COLORS, SOURCE_COLORS
from theme import get_plotly_template
from widgets import NO_MATCH, ah_product_url, render_products_dataframe


def _nutriscore_badge(score: str) -> str:
    active = score.upper()
    boxes = []
    for s in ["A", "B", "C", "D", "E"]:
        color = NUTRISCORE_COLORS[s]
        if s == active:
            box = (
                f'<div style="background:{color}; color:white; font-size:1.4rem; font-weight:900;'
                f' padding:8px 11px; border-radius:5px; display:flex; align-items:center;'
                f' justify-content:center; min-width:34px;">{s}</div>'
            )
        else:
            box = (
                f'<div style="background:{color}44; color:{color}; font-size:0.9rem; font-weight:700;'
                f' padding:5px 8px; border-radius:4px; display:flex; align-items:center;'
                f' justify-content:center; min-width:26px;">{s}</div>'
            )
        boxes.append(box)
    return (
        '<div style="display:flex; align-items:center; gap:3px; margin:6px 0;">'
        '<span style="font-size:0.6rem; font-weight:800; opacity:0.55; letter-spacing:0.05em;'
        ' margin-right:5px; line-height:1.2;">NUTRI<br>SCORE</span>'
        + "".join(boxes)
        + "</div>"
    )


def page_products() -> None:
    st.header("🛒 Products")

    if "web_id" in st.query_params and "selected_product_web_id" not in st.session_state:
        try:
            st.session_state.selected_product_web_id = int(st.query_params["web_id"])
        except (ValueError, TypeError):
            pass

    if "selected_product_web_id" in st.session_state:
        page_product()
        return

    if "selected_product_pos_id" in st.session_state:
        page_product_no_web_id()
        return

    search = st.text_input(
        "🔍 Search products", placeholder="Type to filter...", key="product_search"
    )
    selected_category = st.session_state.get("_products_category")

    if search:
        _products_search_view(search)
    elif selected_category:
        _products_category_view(selected_category)
    else:
        _products_landing()


def _products_search_view(search: str) -> None:
    products = load_products()
    mask = products["title"].fillna("").str.contains(
        search, case=False, na=False
    ) | products["brand"].fillna("").str.contains(search, case=False, na=False)
    filtered = products[mask]

    st.caption(f"{len(filtered)} product(s) matching '{search}'")
    selected_wid = render_products_dataframe(filtered)
    if selected_wid is not None:
        st.session_state.selected_product_web_id = selected_wid
        st.rerun()


def _products_category_view(selected_category: str) -> None:
    col_back, col_title = st.columns([1, 8])
    with col_back:
        if st.button("← Categories"):
            del st.session_state["_products_category"]
            st.rerun()
    with col_title:
        st.subheader(selected_category)

    products = load_products()
    filtered = products[products["ah_category"] == selected_category]
    st.caption(f"{len(filtered)} product(s)")

    selected_wid = render_products_dataframe(filtered)
    if selected_wid is not None:
        st.session_state.selected_product_web_id = selected_wid
        st.rerun()


def _products_landing() -> None:
    products = load_products()
    cats = (
        products[products["title"].notna() & products["ah_category"].notna()]
        .groupby("ah_category", as_index=False)
        .size()
        .rename(columns={"size": "product_count"})
        .sort_values("ah_category")
    )
    if cats.empty:
        st.info("No products cached yet. Run the sync service first.")
        return

    st.caption(
        f"{cats['product_count'].sum()} products in {len(cats)} categories — "
        "click a category to browse"
    )
    cols = st.columns(3)
    for i, row in cats.iterrows():
        cat = row["ah_category"]
        cnt = int(row["product_count"])
        with cols[i % 3]:
            if st.button(f"{cat}  ({cnt})", key=f"cat_{cat}", width="stretch"):
                st.session_state["_products_category"] = cat
                st.rerun()


def page_product() -> None:
    """Individual product detail page."""
    web_id = st.session_state.get("selected_product_web_id")
    if web_id is None:
        st.info("Select a product from the Products page.")
        return

    p = get_product_detail(int(web_id))
    if p is None:
        st.warning(f"Product with web_id {web_id} not found.")
        return

    nav_source = st.session_state.get("_product_nav_source")
    back_label = {"receipts": "← Back to Receipt", "orders": "← Back to Order"}.get(
        nav_source, "← Back to Products"
    )
    if st.button(back_label):
        del st.session_state["selected_product_web_id"]
        st.session_state.pop("_product_nav_source", None)
        if nav_source == "receipts" and "_receipts_page" in st.session_state:
            st.switch_page(st.session_state._receipts_page)
        elif nav_source == "orders" and "_orders_page" in st.session_state:
            st.switch_page(st.session_state._orders_page)
        else:
            st.rerun()

    if not p.get("title"):
        _render_no_data_panel(int(web_id), p.get("pos_id"))
        return

    col_img, col_info = st.columns([1, 4])
    with col_img:
        if p.get("thumbnail_url"):
            st.image(p["thumbnail_url"], width=150)
    with col_info:
        st.header(p["title"] or "Unknown Product")

        meta_parts = []
        if p.get("brand"):
            meta_parts.append(f"**Brand:** {p['brand']}")
        if p.get("ah_category"):
            meta_parts.append(f"**Category:** {p['ah_category']}")
        if p.get("ah_subcategory"):
            meta_parts.append(f"**Subcategory:** {p['ah_subcategory']}")
        if meta_parts:
            st.write(" · ".join(meta_parts))

        tag_parts = []
        if p.get("unit_size"):
            tag_parts.append(f"**Size:** {p['unit_size']}")
        if p.get("net_content"):
            tag_parts.append(f"**Net content:** {p['net_content']}")
        if p.get("serving_size"):
            tag_parts.append(f"**Serving:** {p['serving_size']}")
        if p.get("unit_price_description"):
            tag_parts.append(p["unit_price_description"])
        if tag_parts:
            st.write(" · ".join(tag_parts))
        if p.get("nutriscore"):
            st.markdown(_nutriscore_badge(p["nutriscore"]), unsafe_allow_html=True)

        url = ah_product_url(web_id)
        if url:
            st.markdown(f"[View on ah.nl]({url})")

    _render_refetch_button(int(web_id))

    st.divider()

    effective_weight = p.get("weight_per_unit_kg")
    effective_weight = float(effective_weight) if pd.notna(effective_weight) else None

    _render_co2_warning(p, int(web_id), effective_weight)

    _render_weight_section(p, effective_weight)
    _render_co2_section(p, effective_weight)

    st.divider()
    st.subheader("IDs")
    id_parts = [f"Web ID: {web_id}"]
    pos_id = p.get("pos_id")
    if pos_id is not None:
        pos_info = get_product_by_pos_id(int(pos_id))
        pos_description = pos_info.get("description") if pos_info else None
        pos_label = f"POS ID: {pos_id}"
        if pos_description:
            pos_label += f" ({pos_description})"
        id_parts.append(pos_label)
    else:
        id_parts.append("POS ID: not yet linked")
    st.caption(" · ".join(id_parts))

    st.divider()
    st.subheader("Purchase History")

    all_purchases = pd.DataFrame(get_product_purchases(int(web_id)))
    if not all_purchases.empty:
        all_purchases["date"] = pd.to_datetime(all_purchases["date"], errors="coerce")
        all_purchases = all_purchases.sort_values("date", ascending=False).reset_index(drop=True)
        total_qty = all_purchases["quantity"].sum()
        total_spent = all_purchases["amount"].sum()
        times_bought = len(all_purchases)

        pcols = st.columns(3)
        pcols[0].metric("Times Bought", times_bought)
        pcols[1].metric("Total Quantity", f"{total_qty:.0f}")
        pcols[2].metric("Total Spent", f"€ {total_spent:.2f}")

        display = all_purchases.copy()
        display["date"] = display["date"].dt.strftime("%-d %b %Y")
        st.dataframe(
            display.rename(
                columns={"date": "Date", "description": "Description", "quantity": "Quantity", "amount": "Amount (€)", "source": "Source"}
            )[["Date", "Description", "Quantity", "Amount (€)", "Source"]],
            column_config={"Amount (€)": st.column_config.NumberColumn(format="€ %.2f")},
            hide_index=True,
            width="stretch",
        )

        _render_purchase_charts(all_purchases)
    else:
        st.info("No purchases found for this product.")

    st.divider()
    st.subheader("Edit Enrichment")
    _render_edit_enrichment(p, web_id, p.get("weight_kg"))


def _render_purchase_charts(purchases: pd.DataFrame) -> None:
    dated = purchases.dropna(subset=["date"]).copy().sort_values("date")
    if len(dated) < 2:
        return

    template = get_plotly_template()
    st.subheader("Price per Unit")
    price_data = dated.dropna(subset=["unit_price"])
    if not price_data.empty:
        fig_price = px.line(
            price_data,
            x="date",
            y="unit_price",
            color="source",
            markers=True,
            labels={"date": "Date", "unit_price": "€ per unit", "source": "Source"},
            color_discrete_map=SOURCE_COLORS,
            template=template,
        )
        fig_price.update_layout(xaxis_title=None, yaxis_tickprefix="€", yaxis_tickformat=".2f")
        st.plotly_chart(fig_price, width="stretch")

    st.subheader("Purchase Frequency")
    period_label = st.radio(
        "Period",
        ["1M", "1Q", "1Y"],
        horizontal=True,
        key="purchase_freq_period",
        label_visibility="collapsed",
    )
    period_code = {"1M": "M", "1Q": "Q", "1Y": "Y"}[period_label]
    dated["bucket"] = dated["date"].dt.to_period(period_code).dt.to_timestamp()
    freq = dated.groupby("bucket", as_index=False)["quantity"].sum()
    fig_freq = px.bar(
        freq,
        x="bucket",
        y="quantity",
        labels={"bucket": "", "quantity": "Units Bought"},
        color_discrete_sequence=[CHART_COLORS["spent"]],
        template=template,
    )
    fig_freq.update_layout(xaxis_title=None)
    st.plotly_chart(fig_freq, width="stretch")


def _render_edit_enrichment(p, web_id, weight_override) -> None:
    all_co2_names = [NO_MATCH] + [e["name"] for e in get_co2eq_categories()]
    current_name = p.get("co2eq_name") or NO_MATCH
    current_idx = all_co2_names.index(current_name) if current_name in all_co2_names else 0
    current_weight = float(weight_override) if pd.notna(weight_override) else 0.0

    tab_cat, tab_co2 = st.tabs(["Change category", "Override CO₂/kg"])

    with tab_cat:
        st.caption("Select a new CO₂eq category — the default CO₂/kg for that category is assigned automatically.")
        with st.form("edit_product_category"):
            new_name = st.selectbox("CO₂eq match", all_co2_names, index=current_idx)
            new_weight = st.number_input(
                "Weight override (kg/unit)",
                min_value=0.0,
                value=current_weight,
                step=0.001,
                format="%.4f",
                help="Leave at 0 to use AH unit_size.",
            )
            if new_weight < 0.001:
                new_weight = None

            if st.form_submit_button("Save category"):
                if new_name == NO_MATCH:
                    entry = None
                else:
                    co2_data = get_co2eq_entry(new_name)
                    entry = {
                        "co2eq_name": new_name,
                        "co2eq_category": co2_data["category"] if co2_data else None,
                        "co2eq_per_kg": co2_data["co2eq_per_kg"] if co2_data else None,
                    }
                save_manual_correction(int(web_id), entry, weight_kg=new_weight)
                clear_product_enrichment(int(web_id))
                run_enrichment_with_progress()
                st.cache_data.clear()
                st.rerun()

    with tab_co2:
        st.caption("Set a specific CO₂/kg value for this product, keeping the current category label.")
        with st.form("edit_product_co2"):
            current_co2 = float(p["co2eq_per_kg"]) if pd.notna(p.get("co2eq_per_kg")) else 0.0
            new_co2 = st.number_input(
                "CO₂/kg", min_value=0.0, value=current_co2, step=0.1, format="%.3f"
            )

            if st.form_submit_button("Save CO₂/kg"):
                entry = {
                    "co2eq_name": p.get("co2eq_name") or "",
                    "co2eq_category": p.get("co2eq_category") or "",
                    "co2eq_per_kg": new_co2,
                }
                save_manual_correction(int(web_id), entry, weight_kg=current_weight or None)
                clear_product_enrichment(int(web_id))
                run_enrichment_with_progress()
                st.cache_data.clear()
                st.rerun()


def _render_refetch_button(web_id: int) -> None:
    """Button to re-fetch AH product data and trigger re-enrichment."""
    _, col_btn = st.columns([5, 1])
    with col_btn:
        if st.button("🔄 Refresh AH data", width="stretch", help="Re-fetch product metadata from Albert Heijn and re-run enrichment"):
            result = fetch_product_data(web_id)
            if result.get("found"):
                clear_product_enrichment(web_id)
                st.cache_data.clear()
                run_enrichment_with_progress()
                st.rerun()
            else:
                st.error("Product not found in the AH catalogue.")


WEIGHT_SOURCE_LABELS = {
    "unit_size": "Unit size",
    "net_content": "Net content",
    "serving_size": "Serving size",
    "multipack": "Multipack",
    "default": "Default est.",
    "correction": "Correction",
}

# How the CO₂ subcategory was assigned. Matching is a deterministic AH-subcategory
# → CO₂-subcategory lookup (no fuzzy confidence score), so we describe the route.
MATCH_METHOD_LABELS = {
    "subcategory_direct": "matched via AH subcategory",
    "subcategory_vegan": "matched via AH subcategory (vegan variant)",
    "correction": "set manually (correction)",
    "non_food": "excluded as non-food",
    "unmatched": "no CO₂ subcategory matched",
    "no_product": "no product data",
    "ignored": "ignored",
}


def _weight_source_label(p: dict) -> str:
    src = p.get("weight_source") or ""
    return WEIGHT_SOURCE_LABELS.get(src, src or "—")


_SOLD_BY_WEIGHT_RE = re.compile(r"\bper\s+kilo\b|\bper\s+kg\b", re.IGNORECASE)


def _is_sold_by_weight(p: dict) -> bool:
    """True when the product is priced per kg (loose produce, deli) rather than per unit."""
    unit_size = (p.get("unit_size") or "").strip()
    unit_price_desc = (p.get("unit_price_description") or "").strip()
    return bool(_SOLD_BY_WEIGHT_RE.search(unit_size) or _SOLD_BY_WEIGHT_RE.search(unit_price_desc))


def _render_weight_section(p: dict, effective_weight: float | None) -> None:
    """Resolved per-unit weight and which website field it came from."""
    st.markdown("##### Weight")
    if _is_sold_by_weight(p):
        st.write("Sold by weight (price per kg) — no fixed unit weight, so CO₂ per purchase is taken from the actual amount bought.")
        return
    if effective_weight is None:
        st.write("Per-unit weight not available — add one on the Data Quality page.")
        return
    st.write(
        f"**Per unit:** {effective_weight:.3f} kg · "
        f"**Source:** {_weight_source_label(p)}"
    )


def _render_co2_section(p: dict, effective_weight: float | None) -> None:
    """CO₂ intensity, per-unit footprint, and how the CO₂ subcategory was matched."""
    st.markdown("##### CO₂")
    has_co2 = pd.notna(p.get("co2eq_per_kg"))

    cols = st.columns(2)
    cols[0].metric("CO₂/kg", f"{p['co2eq_per_kg']:.2f} kg" if has_co2 else "—")
    if has_co2 and effective_weight is not None:
        cols[1].metric("CO₂/unit", f"{p['co2eq_per_kg'] * effective_weight:.3f} kg")
    else:
        cols[1].metric("CO₂/unit", "—")

    if not has_co2:
        st.write("No CO₂ subcategory matched.")
        return

    parts = []
    if p.get("co2eq_name"):
        parts.append(f"**CO₂ subcategory:** {p['co2eq_name']}")
    if p.get("co2eq_category"):
        parts.append(f"**Group:** {p['co2eq_category']}")
    method = MATCH_METHOD_LABELS.get(p.get("match_method"), p.get("match_method"))
    if method:
        parts.append(f"*{method}*")
    if parts:
        st.write(" · ".join(parts))


def _render_co2_warning(p: dict, web_id: int, effective_weight: float | None) -> None:
    """Warning banner shown when CO2 data is incomplete, with inline quick-fix forms."""
    missing_co2 = pd.isna(p.get("co2eq_per_kg"))
    missing_weight = effective_weight is None

    if not missing_co2 and not missing_weight:
        return

    issues = []
    if missing_co2:
        issues.append("no CO₂ category matched")
    if missing_weight:
        unit_size_raw = (p.get("unit_size") or "").strip()
        unit_price_desc = (p.get("unit_price_description") or "").strip()
        if _SOLD_BY_WEIGHT_RE.search(unit_size_raw) or _SOLD_BY_WEIGHT_RE.search(unit_price_desc):
            issues.append("sold by weight — CO₂ per purchase cannot be computed automatically")
        else:
            issues.append("weight could not be resolved from any source")

    with st.container(border=True):
        st.warning("**Incomplete CO₂ data** — " + " and ".join(issues) + ".")
        if missing_co2:
            _render_warning_category_form(p, web_id)
        if missing_weight:
            _render_warning_weight_form(p, web_id)


def _render_warning_category_form(p: dict, web_id: int) -> None:
    all_co2_names = [NO_MATCH] + [e["name"] for e in get_co2eq_categories()]
    current_name = p.get("co2eq_name") or NO_MATCH
    current_idx = all_co2_names.index(current_name) if current_name in all_co2_names else 0
    current_weight = float(p["weight_kg"]) if pd.notna(p.get("weight_kg")) else None
    with st.form("co2_warning_category"):
        st.caption("Set CO₂eq category")
        new_name = st.selectbox("CO₂eq category", all_co2_names, index=current_idx, label_visibility="collapsed")
        if st.form_submit_button("Save category"):
            entry = _build_co2_entry(new_name)
            save_manual_correction(web_id, entry, weight_kg=current_weight)
            clear_product_enrichment(web_id)
            run_enrichment_with_progress()
            st.cache_data.clear()
            st.rerun()


def _render_warning_weight_form(p: dict, web_id: int) -> None:
    with st.form("co2_warning_weight"):
        st.caption("Set weight override (kg/unit)")
        new_weight = st.number_input(
            "Weight (kg/unit)",
            min_value=0.001,
            step=0.001,
            format="%.4f",
            label_visibility="collapsed",
        )
        if st.form_submit_button("Save weight"):
            current_entry = None
            if not pd.isna(p.get("co2eq_per_kg")):
                current_entry = {
                    "co2eq_name": p.get("co2eq_name") or "",
                    "co2eq_category": p.get("co2eq_category") or "",
                    "co2eq_per_kg": p["co2eq_per_kg"],
                }
            save_manual_correction(web_id, current_entry, weight_kg=new_weight)
            clear_product_enrichment(web_id)
            run_enrichment_with_progress()
            st.cache_data.clear()
            st.rerun()


def _build_co2_entry(name: str) -> dict | None:
    if name == NO_MATCH:
        return None
    co2_data = get_co2eq_entry(name)
    return {
        "co2eq_name": name,
        "co2eq_category": co2_data["category"] if co2_data else None,
        "co2eq_per_kg": co2_data["co2eq_per_kg"] if co2_data else None,
    }


def _render_no_data_panel(web_id: int, pos_id: int | None = None) -> None:
    """Shown when a product exists in the DB but has no AH metadata yet."""
    pos_info = get_product_by_pos_id(int(pos_id)) if pos_id is not None else None
    pos_description = pos_info.get("description") if pos_info else None

    if pos_description:
        st.header(pos_description)

    st.warning(
        f"No Albert Heijn data found for web ID **{web_id}**. "
        "The product may have been unavailable when last synced."
    )

    id_parts = [f"Web ID: {web_id}"]
    if pos_id is not None:
        id_parts.append(f"POS ID: {pos_id}")
    st.caption(" · ".join(id_parts))

    url = ah_product_url(web_id)
    if url:
        st.markdown(f"[View on ah.nl]({url})")
    if st.button("🔄 Fetch data from AH"):
        result = fetch_product_data(web_id)
        if result.get("found"):
            st.success(f"Found: **{result['title']}**. Reloading…")
            st.cache_data.clear()
            st.rerun()
        else:
            st.error("Product not found in the AH catalogue.")

    if pos_id is not None:
        st.divider()
        st.subheader("Link to different web ID")
        with st.form("relink_pos_id"):
            raw = st.text_input(
                "AH product web ID or URL",
                placeholder="e.g. 810985 or https://www.ah.nl/producten/product/wi810985/…",
            )
            if st.form_submit_button("Link product"):
                new_web_id = _parse_ah_web_id(raw)
                if new_web_id is None:
                    st.error("Could not parse a web ID from the input.")
                else:
                    result = link_product_pos_id(int(pos_id), new_web_id)
                    title = result.get("product_title")
                    if title:
                        st.success(f"Linked to **{title}**. Reloading…")
                    else:
                        st.info(f"Linked web ID {new_web_id} (product data not yet available).")
                    st.cache_data.clear()
                    st.session_state["selected_product_web_id"] = new_web_id
                    st.rerun()


def page_product_no_web_id() -> None:
    """Shown when an item has a POS product_id but no AH web_id."""
    pos_id = st.session_state.get("selected_product_pos_id")
    if pos_id is None:
        st.info("Select a product from the Products page.")
        return

    nav_source = st.session_state.get("_product_nav_source")
    back_label = {"receipts": "← Back to Receipt", "orders": "← Back to Order"}.get(
        nav_source, "← Back to Products"
    )
    if st.button(back_label):
        del st.session_state["selected_product_pos_id"]
        st.session_state.pop("_product_nav_source", None)
        if nav_source == "receipts" and "_receipts_page" in st.session_state:
            st.switch_page(st.session_state._receipts_page)
        elif nav_source == "orders" and "_orders_page" in st.session_state:
            st.switch_page(st.session_state._orders_page)
        else:
            st.rerun()

    info = get_product_by_pos_id(int(pos_id))
    description = info["description"] if info else f"POS ID {pos_id}"
    st.header(description)
    st.warning(
        f"This item (POS ID **{pos_id}**) has no Albert Heijn web ID linked. "
        "Enter the AH product web ID or URL to link it."
    )

    with st.form("link_pos_id"):
        raw = st.text_input(
            "AH product web ID or URL",
            placeholder="e.g. 810985 or https://www.ah.nl/producten/product/wi810985/…",
        )
        if st.form_submit_button("Link product"):
            web_id = _parse_ah_web_id(raw)
            if web_id is None:
                st.error("Could not parse a web ID from the input.")
            else:
                result = link_product_pos_id(int(pos_id), web_id)
                title = result.get("product_title")
                if title:
                    st.success(f"Linked to **{title}**. Reloading…")
                else:
                    st.info(f"Linked web ID {web_id} (product data not yet available).")
                st.cache_data.clear()
                del st.session_state["selected_product_pos_id"]
                st.session_state["selected_product_web_id"] = web_id
                st.rerun()


def _parse_ah_web_id(s: str) -> int | None:
    """Extract an AH web ID from a bare integer, 'wi<n>' code, or ah.nl product URL."""
    s = s.strip()
    m = re.search(r"wi(\d+)", s)
    if m:
        return int(m.group(1))
    try:
        return int(s)
    except ValueError:
        return None
