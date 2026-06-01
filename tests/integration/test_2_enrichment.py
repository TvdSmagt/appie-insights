"""Integration tests: validate enrichment pipeline on the synced DB."""

import pytest


@pytest.mark.integration
class TestEnrichment:
    def test_enrichment_creates_rows(self, enriched_db, enriched_db_conn):
        count = enriched_db_conn.execute(
            "SELECT COUNT(*) FROM product_enrichment"
        ).fetchone()[0]
        assert count > 0, "No product_enrichment rows after enrichment"

    def test_food_products_have_co2eq(self, enriched_db, enriched_db_conn):
        """Food products (non-ignored) should have a co2eq_per_kg value."""
        total_food = enriched_db_conn.execute("""
            SELECT COUNT(*) FROM product_enrichment
            WHERE match_method != 'ignored'
        """).fetchone()[0]

        with_co2eq = enriched_db_conn.execute("""
            SELECT COUNT(*) FROM product_enrichment
            WHERE match_method != 'ignored' AND co2eq_per_kg IS NOT NULL
        """).fetchone()[0]

        if total_food == 0:
            pytest.skip("No non-ignored products to check")

        ratio = with_co2eq / total_food
        assert ratio >= 0.5, (
            f"Only {with_co2eq}/{total_food} food products have co2eq_per_kg "
            f"({ratio:.0%}); expected ≥50%"
        )

    def test_co2eq_values_are_positive(self, enriched_db, enriched_db_conn):
        bad = enriched_db_conn.execute("""
            SELECT web_id, co2eq_per_kg FROM product_enrichment
            WHERE co2eq_per_kg IS NOT NULL AND co2eq_per_kg <= 0
        """).fetchall()
        assert bad == [], f"Products with non-positive co2eq_per_kg: {[(r[0], r[1]) for r in bad]}"

    def test_match_method_set(self, enriched_db, enriched_db_conn):
        missing = enriched_db_conn.execute("""
            SELECT COUNT(*) FROM product_enrichment
            WHERE match_method IS NULL OR match_method = ''
        """).fetchone()[0]
        assert missing == 0, f"{missing} enriched rows have no match_method"

    def test_known_match_methods(self, enriched_db, enriched_db_conn):
        methods = {
            r[0] for r in enriched_db_conn.execute(
                "SELECT DISTINCT match_method FROM product_enrichment"
            ).fetchall()
        }
        valid = {
            "correction", "no_metadata", "unmatched", "ignored",
            "subcategory_direct", "subcategory_unmapped", "no_product", "non_food",
        }
        unknown = methods - valid
        assert not unknown, f"Unexpected match methods: {unknown}"

    def test_all_referenced_products_enriched(self, enriched_db, enriched_db_conn):
        """Every web_id referenced by an item or order_item should have an enrichment row.

        Products stubs that exist in the products table but aren't referenced by
        any purchase item are intentionally skipped by the enrichment pipeline.
        """
        missing = enriched_db_conn.execute("""
            SELECT COUNT(*) FROM (
                SELECT DISTINCT web_id FROM items       WHERE web_id IS NOT NULL
                UNION
                SELECT web_id             FROM order_items WHERE web_id IS NOT NULL
            ) AS referenced
            LEFT JOIN product_enrichment pe ON pe.web_id = referenced.web_id
            WHERE pe.web_id IS NULL
        """).fetchone()[0]
        assert missing == 0, f"{missing} referenced products have no enrichment row"
