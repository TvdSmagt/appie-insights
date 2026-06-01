"""Integration tests: validate the DB state after running the sync binary."""

import pytest


@pytest.mark.integration
class TestReceiptSync:
    def test_receipts_present(self, db_conn):
        count = db_conn.execute("SELECT COUNT(*) FROM receipts").fetchone()[0]
        assert count > 0, "Expected at least one receipt to be synced"

    def test_receipts_have_date_and_total(self, db_conn):
        bad = db_conn.execute("""
            SELECT transaction_id FROM receipts
            WHERE date IS NULL OR date = '' OR total_amount IS NULL OR total_amount = 0
        """).fetchall()
        assert bad == [], f"Receipts missing date or total_amount: {[r[0] for r in bad]}"

    def test_items_present(self, db_conn):
        count = db_conn.execute("SELECT COUNT(*) FROM items").fetchone()[0]
        assert count > 0, "Expected receipt items to be synced"

    def test_items_reference_valid_receipts(self, db_conn):
        orphans = db_conn.execute("""
            SELECT i.id FROM items i
            LEFT JOIN receipts r ON r.transaction_id = i.receipt_id
            WHERE r.transaction_id IS NULL
        """).fetchall()
        assert orphans == [], f"Items with no matching receipt: {[r[0] for r in orphans]}"

    def test_items_have_descriptions(self, db_conn):
        blank = db_conn.execute("""
            SELECT COUNT(*) FROM items WHERE description IS NULL OR description = ''
        """).fetchone()[0]
        assert blank == 0, f"{blank} items have no description"

    def test_products_created_for_web_ids(self, db_conn):
        """Items with a web_id should have a corresponding products stub."""
        missing = db_conn.execute("""
            SELECT COUNT(DISTINCT i.web_id) FROM items i
            LEFT JOIN products p ON p.web_id = i.web_id
            WHERE i.web_id IS NOT NULL AND p.web_id IS NULL
        """).fetchone()[0]
        assert missing == 0, f"{missing} web_ids from items have no products row"

    def test_products_have_details(self, db_conn):
        """Products fetched from the API should have a title."""
        total = db_conn.execute(
            "SELECT COUNT(*) FROM products WHERE fetched_at IS NOT NULL AND title IS NOT NULL"
        ).fetchone()[0]
        assert total > 0, "Expected at least some products to have fetched details"



@pytest.mark.integration
class TestOrderSync:
    def test_orders_present_or_skipped(self, db_conn):
        """Orders are optional (not all accounts have online orders)."""
        count = db_conn.execute("SELECT COUNT(*) FROM orders").fetchone()[0]
        # Not asserting > 0 since the test account may not have delivery orders;
        # just verify the table is readable.
        assert count >= 0

    def test_order_items_reference_valid_orders(self, db_conn):
        orphans = db_conn.execute("""
            SELECT oi.id FROM order_items oi
            LEFT JOIN orders o ON o.order_id = oi.order_id
            WHERE o.order_id IS NULL
        """).fetchall()
        assert orphans == [], f"Order items with no matching order: {[r[0] for r in orphans]}"
