import math

import pandas as pd
import streamlit as st

from backend_client import (
    _get as _backend_get,
    clear_all_enrichment,
    delete_correction,
    get_co2eq_categories,
    get_co2eq_entry,
    get_product_issues,
    get_products,
    link_product_pos_id,
    run_enrichment_with_progress,
    save_manual_correction,
)
from widgets import NO_MATCH, ah_product_url
from pages.products import _parse_ah_web_id


def _product_page_url(web_id: int) -> str:
    return f"/page_products?web_id={web_id}"


def page_data_quality() -> None:
    st.header("🧹 Data Quality")

    try:
        issues = get_product_issues()
    except Exception as e:
        st.error(f"Could not load product issues: {e}")
        return

    summary = issues.get("summary", {})

    # --- Overview metrics ---
    cols = st.columns(6)
    cols[0].metric("Food products", summary.get("total_food_products", 0))
    cols[1].metric("POS → no web ID", summary.get("no_web_id", 0), help="Receipt items with a POS id not yet linked to an AH web product")
    cols[2].metric("Web → no POS id", summary.get("no_pos_id", 0), help="Products with a web id but no POS barcode linked")
    cols[3].metric("No product data", summary.get("no_product_data", 0), help="Products where AH site data (title, subcategory) was never fetched")
    cols[4].metric("No weight", summary.get("no_weight", 0), help="Enriched products whose weight can't be determined from unit_size")
    cols[5].metric("Unmapped subcats", summary.get("unmatched_subcategories", 0), help="Food subcategories not yet mapped to a CO₂ category — each needs a decision")

    st.divider()

    _section_no_web_id(issues.get("no_web_id", []))

    st.divider()
    _section_no_product_data(issues.get("no_product_data", []))

    st.divider()
    _section_no_weight(issues.get("no_weight", []))

    st.divider()
    _section_no_pos_id(issues.get("no_pos_id", []))

    st.divider()
    _section_unmatched_subcategories(issues.get("unmatched_subcategories", []))

    st.divider()
    _section_unmatched_no_subcategory(issues.get("unmatched_no_subcategory", []))

    st.divider()
    _section_existing_corrections()


# --- Section helpers ---

def _section_no_web_id(items: list[dict]) -> None:
    st.subheader("1. POS ID → No Web ID")
    st.caption("Receipt items that have a POS barcode but have not yet been linked to an AH web product.")

    if not items:
        st.success("All receipt items are linked to a web product. ✓")
        return

    st.warning(f"{len(items)} unlinked receipt item(s).")

    df = pd.DataFrame(items)
    df["web_id_input"] = ""

    edited = st.data_editor(
        df[["pos_id", "description", "web_id_input"]].rename(columns={
            "pos_id": "POS ID",
            "description": "Description",
            "web_id_input": "AH web ID or URL",
        }),
        column_config={
            "POS ID": st.column_config.NumberColumn(disabled=True),
            "Description": st.column_config.TextColumn(disabled=True),
            "AH web ID or URL": st.column_config.TextColumn(
                help="Paste the AH product URL or enter the numeric web ID"
            ),
        },
        key="no_web_id_editor",
        hide_index=True,
        width="stretch",
    )

    if st.button("Link products", key="save_no_web_id"):
        n_saved = 0
        for i, row in edited.iterrows():
            raw = row.get("AH web ID or URL", "").strip()
            if not raw:
                continue
            web_id = _parse_ah_web_id(raw)
            if web_id is None:
                st.error(f"Row {i+1}: could not parse a web ID from '{raw}'")
                continue
            pos_id = int(df.iloc[i]["pos_id"])
            result = link_product_pos_id(pos_id, web_id)
            n_saved += 1
            title = result.get("product_title")
            if title:
                st.success(f"POS {pos_id} → **{title}** (web {web_id})")
            else:
                st.info(f"POS {pos_id} linked to web {web_id} (product data not yet available).")
        if n_saved:
            st.cache_data.clear()
            st.rerun()
        else:
            st.info("No changes — enter a web ID or URL for at least one row.")


def _section_no_pos_id(items: list[dict]) -> None:
    st.subheader("4. Web ID → No POS ID")
    st.caption("Food products with an AH web ID but no POS barcode linked yet.")

    if not items:
        st.success("All food products have a POS ID linked. ✓")
        return

    st.info(f"{len(items)} product(s) have no POS ID. These can be linked when seen in a future receipt, or manually on the product page.")

    df = pd.DataFrame(items)
    df["ah_url"] = df["web_id"].apply(ah_product_url)
    df["product_page"] = df["web_id"].apply(_product_page_url)

    _render_issue_table(
        df,
        cols=["title", "ah_subcategory", "ah_category", "unit_size", "co2eq_name", "product_page", "ah_url"],
        rename={
            "title": "Title",
            "ah_subcategory": "Subcategory",
            "ah_category": "Category",
            "unit_size": "Unit size",
            "co2eq_name": "CO₂ match",
            "product_page": "Product page",
            "ah_url": "AH.nl",
        },
        link_cols={"product_page": "Open", "ah_url": "View"},
        key="no_pos_id_table",
    )


def _section_no_product_data(items: list[dict]) -> None:
    st.subheader("2. No Product Data")
    st.caption("Products with a web ID where AH site data (title, subcategory) has not been fetched yet.")

    if not items:
        st.success("All products have AH site data. ✓")
        return

    st.warning(f"{len(items)} product(s) have no AH data. Open each on the product page to fetch it.")

    df = pd.DataFrame(items)
    df["ah_url"] = df["web_id"].apply(ah_product_url)
    df["product_page"] = df["web_id"].apply(_product_page_url)

    _render_issue_table(
        df,
        cols=["web_id", "pos_id", "pos_description", "product_page", "ah_url"],
        rename={
            "web_id": "Web ID",
            "pos_id": "POS ID",
            "pos_description": "POS Description",
            "product_page": "Product page",
            "ah_url": "AH.nl",
        },
        link_cols={"product_page": "Open", "ah_url": "View"},
        key="no_product_data_table",
    )


def _section_no_weight(items: list[dict]) -> None:
    st.subheader("3. No Weight")
    st.caption("Enriched food products whose weight cannot be determined from unit_size. Weight defaults to 1 kg.")

    if not items:
        st.success("All enriched products have weight data. ✓")
        return

    st.warning(f"{len(items)} product(s) missing weight information.")

    df = pd.DataFrame(items)

    # Top 3 subcategories
    if "ah_subcategory" in df.columns:
        top_subcats = (
            df["ah_subcategory"]
            .replace("", pd.NA)
            .dropna()
            .value_counts()
            .head(3)
        )
        if not top_subcats.empty:
            st.write("**Top subcategories:**")
            subcat_cols = st.columns(len(top_subcats))
            for col, (subcat, count) in zip(subcat_cols, top_subcats.items()):
                col.metric(subcat, count)

    df["ah_url"] = df["web_id"].apply(ah_product_url)
    df["product_page"] = df["web_id"].apply(_product_page_url)
    df["corrected_weight_kg"] = pd.to_numeric(df["weight_kg"], errors="coerce")

    edited = st.data_editor(
        df[["title", "ah_category", "ah_subcategory", "unit_size", "co2eq_category", "co2eq_per_kg", "corrected_weight_kg", "product_page", "ah_url"]].rename(columns={
            "title": "Title",
            "ah_category": "Category",
            "ah_subcategory": "Subcategory",
            "unit_size": "Unit size",
            "co2eq_category": "CO₂ category",
            "co2eq_per_kg": "CO₂/kg",
            "corrected_weight_kg": "Weight (kg/unit)",
            "product_page": "Product page",
            "ah_url": "AH.nl",
        }),
        column_config={
            "Title": st.column_config.TextColumn(disabled=True),
            "Category": st.column_config.TextColumn(disabled=True),
            "Subcategory": st.column_config.TextColumn(disabled=True),
            "Unit size": st.column_config.TextColumn(disabled=True),
            "CO₂ category": st.column_config.TextColumn(disabled=True),
            "CO₂/kg": st.column_config.NumberColumn(disabled=True, format="%.2f"),
            "Weight (kg/unit)": st.column_config.NumberColumn(format="%.4f", min_value=0.001),
            "Product page": st.column_config.LinkColumn("Product page", display_text="Open"),
            "AH.nl": st.column_config.LinkColumn("AH.nl", display_text="View"),
        },
        key="no_weight_editor",
        hide_index=True,
        width="stretch",
    )

    if st.button("Save weight fixes", key="save_weight_fixes"):
        n_saved = _save_weight_fixes(df, edited)
        if n_saved:
            st.success(f"Saved {n_saved} weight fix(es).")
            st.cache_data.clear()
            st.rerun()
        else:
            st.info("No changes detected.")


def _save_weight_fixes(orig_df: pd.DataFrame, edited_df: pd.DataFrame) -> int:
    n_saved = 0
    for i, row in edited_df.iterrows():
        new_weight = row.get("Weight (kg/unit)")
        if new_weight is None or (isinstance(new_weight, float) and math.isnan(new_weight)):
            continue
        orig = orig_df.iloc[i]
        orig_w = orig["corrected_weight_kg"]
        if pd.notna(orig_w) and float(new_weight) == float(orig_w):
            continue

        web_id = int(orig["web_id"])
        co2_name = orig.get("co2eq_name") or ""
        co2_cat = orig.get("co2eq_category") or ""
        co2_per_kg = orig.get("co2eq_per_kg")

        entry = (
            {"co2eq_name": co2_name, "co2eq_category": co2_cat, "co2eq_per_kg": co2_per_kg}
            if co2_name
            else None
        )
        save_manual_correction(web_id, entry, weight_kg=float(new_weight))
        n_saved += 1
    return n_saved


def _render_issue_table(
    df: pd.DataFrame,
    cols: list[str],
    rename: dict[str, str],
    link_cols: dict[str, str],
    key: str,
) -> None:
    column_config = {}
    for col, display_text in link_cols.items():
        label = rename.get(col, col)
        column_config[label] = st.column_config.LinkColumn(label, display_text=display_text)
    for col in cols:
        label = rename.get(col, col)
        if label not in column_config:
            column_config[label] = st.column_config.TextColumn(disabled=True)

    present = [c for c in cols if c in df.columns]
    st.dataframe(
        df[present].rename(columns=rename),
        column_config=column_config,
        hide_index=True,
        width="stretch",
        key=key,
    )


def _section_unmatched_subcategories(items: list[dict]) -> None:
    st.subheader("5. Unmapped Subcategories")
    st.caption(
        "Food products whose AH subcategory is not yet in the subcategory map, so they get no CO₂ factor "
        "(`match_method = unmatched`). The subcategory — not the individual product — is the unit of decision: "
        "each row below is one subcategory that needs categorizing."
    )

    if not items:
        st.success("Every AH subcategory in use is mapped. ✓")
        return

    n_products = sum(int(it.get("product_count", 0)) for it in items)
    st.warning(f"{len(items)} subcategory(ies) need a decision, affecting {n_products} product(s).")

    st.markdown(
        "For each subcategory, decide in `backend/data/ah_subcategory_map.csv`:\n"
        "- **Food** → add a row mapping it to a CO₂ category with a `co2eq_per_kg` value.\n"
        "- **Known non-food** → add a row with a blank `co2eq_per_kg` (it becomes `non_food` instead of `unmatched`).\n\n"
        "Re-run enrichment afterwards to apply the change to every affected product."
    )

    df = pd.DataFrame(items)
    df["examples"] = df.get("example_titles", pd.Series([[]] * len(df))).apply(
        lambda titles: ", ".join(titles) if isinstance(titles, list) else ""
    )

    _render_issue_table(
        df,
        cols=["ah_subcategory", "ah_category", "product_count", "examples"],
        rename={
            "ah_subcategory": "Subcategory",
            "ah_category": "Category",
            "product_count": "Affected products",
            "examples": "Example products",
        },
        link_cols={},
        key="unmatched_subcategories_table",
    )


def _section_unmatched_no_subcategory(items: list[dict]) -> None:
    st.subheader("6. Unmatched — No Subcategory")
    st.caption(
        "Food products that ended up `unmatched` but carry *no AH subcategory string at all*, so there is nothing to "
        "map in section 5. Fix these by (re)fetching their AH product data on the product page, or with a per-product "
        "correction. Once a subcategory comes in, any still-unmapped one surfaces in section 5."
    )

    if not items:
        st.success("Every unmatched product has an AH subcategory. ✓")
        return

    st.warning(f"{len(items)} unmatched product(s) have no subcategory to map.")

    df = pd.DataFrame(items)
    df["ah_url"] = df["web_id"].apply(ah_product_url)
    df["product_page"] = df["web_id"].apply(_product_page_url)

    _render_issue_table(
        df,
        cols=["title", "ah_category", "unit_size", "pos_description", "product_page", "ah_url"],
        rename={
            "title": "Title",
            "ah_category": "Category",
            "unit_size": "Unit size",
            "pos_description": "POS description",
            "product_page": "Product page",
            "ah_url": "AH.nl",
        },
        link_cols={"product_page": "Open", "ah_url": "View"},
        key="unmatched_no_subcategory_table",
    )


def _section_existing_corrections() -> None:
    st.subheader("7. Existing Corrections")
    try:
        statuses = _backend_get("/corrections/redundancy")
    except Exception as e:
        st.error(f"Could not load corrections: {e}")
        return

    if not statuses:
        st.info("No corrections defined yet.")
    else:
        try:
            title_map = {p["web_id"]: p["title"] for p in get_products()}
        except Exception:
            title_map = {}

        df = pd.DataFrame(statuses)
        if "web_id" in df.columns:
            df.insert(1, "product_name", df["web_id"].map(title_map).fillna(""))
        if "redundant" in df.columns:
            df.insert(2, "status", df["redundant"].map(lambda r: "auto-matched" if r else "active"))
            df = df.drop(columns=["redundant"])
        st.dataframe(df, width="stretch", hide_index=True)

        redundant = [s for s in statuses if s.get("redundant")]
        if redundant:
            st.markdown(
                f"**{len(redundant)} redundant correction(s)** — the category is now derived automatically "
                "and the correction can be deleted:"
            )
            for s in redundant:
                web_id = s["web_id"]
                name = title_map.get(web_id) or f"#{web_id}"
                col_info, col_btn = st.columns([5, 1])
                col_info.markdown(
                    f"**{name}** (#{web_id}) &mdash; "
                    f"{s.get('co2eq_category', '')} / {s.get('co2eq_name', '')} "
                    f"via `{s.get('auto_method', '')}`"
                )
                if col_btn.button("Delete", key=f"del_correction_{web_id}"):
                    delete_correction(web_id)
                    st.cache_data.clear()
                    st.rerun()

    st.divider()
    if st.button("♻️ Re-enrich all items with current corrections"):
        clear_all_enrichment()
        run_enrichment_with_progress()
        st.cache_data.clear()
        st.rerun()
