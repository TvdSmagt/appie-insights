import logging
import os
import time
from urllib.parse import quote

import requests
import streamlit as st

log = logging.getLogger(__name__)

BACKEND_URL = os.environ.get("BACKEND_URL", "http://enrich:8001")


def _get(path: str) -> list | dict:
    resp = requests.get(f"{BACKEND_URL}{path}", timeout=5)
    resp.raise_for_status()
    return resp.json()


def _post(path: str, data) -> dict:
    resp = requests.post(f"{BACKEND_URL}{path}", json=data, timeout=10)
    resp.raise_for_status()
    return resp.json()


def _delete(path: str) -> dict:
    resp = requests.delete(f"{BACKEND_URL}{path}", timeout=5)
    resp.raise_for_status()
    return resp.json() if resp.content else {}


def get_version() -> str | None:
    """Returns the backend version string, or None if the backend is unreachable."""
    try:
        return _get("/version").get("version")
    except Exception:
        return None


def get_auth_status() -> dict | None:
    """Returns {"logged_in": bool, "in_progress": bool, "error": str}, or None if backend unreachable."""
    try:
        return _get("/auth/status")
    except Exception:
        return None


def get_sync_status() -> dict | None:
    """Returns the current sync status, or None if the backend is unreachable."""
    try:
        return _get("/sync/status")
    except Exception:
        return None


def start_login() -> None:
    """Triggers the OAuth login flow on the backend (opens a browser window)."""
    _post("/login/start", {})


def logout() -> None:
    """Clears AH credentials on the backend."""
    _post("/logout", {})


@st.cache_data(ttl=3600)
def get_co2eq_categories() -> list[dict]:
    try:
        return _get("/categories")
    except Exception:
        log.warning("Could not load CO2eq categories from backend")
        return []


def get_co2eq_entry(name: str) -> dict | None:
    for entry in get_co2eq_categories():
        if entry["name"] == name:
            return entry
    return None


# --- Analytics data fetching ---

def get_receipts() -> list[dict]:
    return _get("/receipts")


def get_receipt_detail(receipt_id: str) -> dict | None:
    try:
        return _get(f"/receipts/{receipt_id}")
    except requests.HTTPError as e:
        if e.response.status_code == 404:
            return None
        raise


def get_items() -> list[dict]:
    return _get("/items")


def get_orders() -> list[dict]:
    return _get("/orders")


def get_order_detail(order_id: int) -> dict | None:
    try:
        return _get(f"/orders/{order_id}")
    except requests.HTTPError as e:
        if e.response.status_code == 404:
            return None
        raise


def get_products() -> list[dict]:
    return _get("/products")


def get_product_stats(since: str | None = None) -> list[dict]:
    path = f"/products/stats?since={since}" if since else "/products/stats"
    return _get(path)


def get_product_detail(web_id: int) -> dict | None:
    try:
        return _get(f"/products/{web_id}")
    except requests.HTTPError as e:
        if e.response.status_code in {404, 500}:
            log.warning(
                "Could not load product detail for web_id=%s: backend returned %s",
                web_id,
                e.response.status_code,
            )
            return None
        raise


def get_product_purchases(web_id: int) -> list[dict]:
    return _get(f"/products/{web_id}/purchases")


def get_product_by_pos_id(pos_id: int) -> dict | None:
    try:
        return _get(f"/pos/{pos_id}")
    except requests.HTTPError as e:
        if e.response.status_code == 404:
            return None
        raise


def fetch_product_data(web_id: int) -> dict:
    """Call POST /products/{web_id}/fetch to (re-)fetch AH product data.

    Returns {"found": bool, "title": str | None}.
    """
    return _post(f"/products/{web_id}/fetch", {})


def link_product_pos_id(pos_id: int, web_id: int) -> dict:
    """Link a POS product_id to an AH web_id and fetch product metadata.

    Returns {"updated_items": int, "product_title": str | None}.
    """
    return _post("/products/link-pos", {"pos_id": pos_id, "web_id": web_id})


def get_nutriscore_distribution(since: str | None = None) -> list[dict]:
    path = f"/products/nutriscores?since={since}" if since else "/products/nutriscores"
    return _get(path)


def get_nutriscore_products(score: str, since: str | None = None) -> list[dict]:
    path = f"/products/nutriscores?score={score}"
    if since:
        path += f"&since={since}"
    return _get(path)


def get_product_issues() -> dict:
    return _get("/products/issues")


def get_missing_category() -> list[dict]:
    return _get("/corrections/missing-category")


def get_missing_weight() -> list[dict]:
    return _get("/corrections/missing-weight")


def search_all(q: str) -> dict:
    return _get(f"/search?q={quote(q)}")


def clear_all_enrichment() -> None:
    _delete("/enrichment")


def reset_database() -> None:
    resp = requests.post(f"{BACKEND_URL}/database/reset", timeout=10)
    resp.raise_for_status()


def clear_product_enrichment(web_id: int) -> int:
    result = _delete(f"/enrichment/{web_id}")
    return result.get("deleted", 0)


def get_financial_summary(since: str | None = None) -> dict:
    path = f"/finances/summary?since={since}" if since else "/finances/summary"
    return _get(path)


def get_spending_by_category(since: str | None = None) -> list[dict]:
    path = f"/finances/by-category?since={since}" if since else "/finances/by-category"
    return _get(path)


def get_spending_over_time(period: str = "month", since: str | None = None) -> list[dict]:
    path = f"/finances/over-time?period={period}"
    if since:
        path += f"&since={since}"
    return _get(path)


def get_top_discounts(since: str | None = None) -> list[dict]:
    path = f"/finances/top-discounts?since={since}" if since else "/finances/top-discounts"
    return _get(path)


def get_sustainability_summary(since: str | None = None, household_ae: float = 1.0) -> dict:
    params = []
    if since:
        params.append(f"since={since}")
    if household_ae != 1.0:
        params.append(f"household_ae={household_ae}")
    path = "/sustainability/summary" + (f"?{'&'.join(params)}" if params else "")
    return _get(path)


def get_sustainability_trend(period: str = "month", since: str | None = None) -> list[dict]:
    params = [f"period={period}"]
    if since:
        params.append(f"since={since}")
    return _get(f"/sustainability/trend?{'&'.join(params)}")


def get_sustainability_categories() -> list[dict]:
    return _get("/sustainability/categories")


def get_category_products(
    category: str,
    period_type: str | None = None,
    period_label: str | None = None,
) -> list[dict]:
    params = []
    if period_type:
        params.append(f"period_type={quote(period_type)}")
    if period_label:
        params.append(f"period_label={quote(period_label)}")
    path = f"/sustainability/categories/{quote(category)}/products"
    if params:
        path += f"?{'&'.join(params)}"
    return _get(path)


def save_manual_correction(
    web_id: int | None,
    entry: dict | None,
    weight_kg: float | None = None,
) -> int:
    """Save a correction via the enrichment service and clear the stale enrichment row.

    Returns count of enrichment rows cleared (worker will re-process).
    """
    corrections = _get("/corrections")

    if entry is None:
        new_correction = {
            "web_id": web_id,
            "action": "ignore",
            "co2eq_per_kg": None,
            "weight_kg": None,
            "co2eq_category": "",
            "co2eq_name": "",
            "notes": "",
        }
    else:
        new_correction = {
            "web_id": web_id,
            "action": "set_category",
            "co2eq_per_kg": entry.get("co2eq_per_kg"),
            "weight_kg": weight_kg or entry.get("weight_kg"),
            "co2eq_category": entry.get("co2eq_category") or entry.get("category") or "",
            "co2eq_name": entry.get("co2eq_name") or entry.get("name") or "",
            "notes": entry.get("note") or "",
        }

    found = False
    for i, c in enumerate(corrections):
        if c["web_id"] == web_id:
            corrections[i] = new_correction
            found = True
            break
    if not found:
        corrections.append(new_correction)

    _post("/corrections", corrections)

    if web_id is not None:
        return clear_product_enrichment(web_id)
    return 0


def delete_correction(web_id: int) -> int:
    """Remove a correction entry and clear the product's enrichment row.

    Returns the count of enrichment rows cleared (worker will re-process with
    automatic matching).
    """
    corrections = _get("/corrections")
    corrections = [c for c in corrections if c["web_id"] != web_id]
    _post("/corrections", corrections)
    return clear_product_enrichment(web_id)


def trigger_sync() -> None:
    """Enqueue a sync cycle (fetches new receipts and orders from AH)."""
    _post("/sync", {})


def run_enrichment_with_progress(receipt_id: str | None = None) -> int:
    """Trigger enrichment via HTTP and poll progress until done."""
    label = "Re-enriching receipt…" if receipt_id else "Enriching new items…"
    body = {"receipt_id": receipt_id} if receipt_id else {}

    try:
        _post("/enrich", body)
    except requests.RequestException as exc:
        st.error(f"Could not reach enrichment service: {exc}")
        return 0

    items_processed = 0
    with st.status(label, expanded=True) as status:
        for _ in range(4):
            time.sleep(0.5)
            try:
                data = _get("/status")
                if data.get("status") == "running":
                    break
            except requests.RequestException:
                pass

        while True:
            try:
                data = _get("/status")
            except requests.RequestException as exc:
                status.write(f"⚠️ {exc}")
                break

            items_total = data.get("items_total", 0)
            items_processed = data.get("items_processed", 0)

            if items_total > 0:
                status.write(f"`{items_processed}/{items_total}` items processed")

            if data.get("status") == "idle":
                break

            time.sleep(0.5)

    status.update(
        label=f"✅ Done — {items_processed} items enriched",
        state="complete",
        expanded=False,
    )
    return items_processed
