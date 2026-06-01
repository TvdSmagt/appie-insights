package syncer

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	appie "github.com/gwillem/appie-go"
)

func syncReceipts(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration, maxReceipts int, progress func(found, synced int)) error {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	receipts, err := client.GetReceipts(reqCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("GetReceipts: %w", err)
	}
	slog.Info("found receipts from AH", "count", len(receipts))

	if err := backfillReceiptHeaders(db, receipts); err != nil {
		slog.Warn("backfill receipt headers", "err", err)
	}

	var toSync []appie.Receipt
	for _, r := range receipts {
		exists, err := receiptExists(db, r.TransactionID)
		if err != nil {
			return fmt.Errorf("check existing %s: %w", r.TransactionID, err)
		}
		if !exists {
			toSync = append(toSync, r)
		}
	}
	if maxReceipts > 0 && len(toSync) > maxReceipts {
		toSync = toSync[:maxReceipts]
	}
	slog.Info("receipt sync status", "already_synced", len(receipts)-len(toSync), "new", len(toSync))
	progress(len(toSync), 0)

	var synced int
	for _, r := range toSync {
		time.Sleep(delay)

		reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
		detail, err := client.GetReceipt(reqCtx, r.TransactionID)
		cancel()
		if err != nil {
			slog.Warn("skip receipt", "transaction_id", r.TransactionID, "err", err)
			continue
		}

		if detail.Date == "" {
			detail.Date = r.Date
		}
		if detail.TotalAmount == 0 {
			detail.TotalAmount = r.TotalAmount
		}

		if err := storeReceipt(db, detail); err != nil {
			return fmt.Errorf("store %s: %w", r.TransactionID, err)
		}

		synced++
		progress(len(toSync), synced)
		slog.Info("synced receipt",
			"transaction_id", r.TransactionID,
			"date", r.Date,
			"total", r.TotalAmount,
			"items", len(detail.Items))
	}

	slog.Info("sync complete", "new_receipts", synced)
	return nil
}

func backfillReceiptHeaders(db *sql.DB, receipts []appie.Receipt) error {
	stmt, err := db.Prepare(`
		UPDATE receipts
		SET date = ?, total_amount = ?
		WHERE transaction_id = ? AND (date = '' OR date IS NULL OR total_amount = 0 OR total_amount IS NULL)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	var updated int
	for _, r := range receipts {
		if r.Date == "" && r.TotalAmount == 0 {
			continue
		}
		res, err := stmt.Exec(r.Date, r.TotalAmount, r.TransactionID)
		if err != nil {
			return fmt.Errorf("update %s: %w", r.TransactionID, err)
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}
	if updated > 0 {
		slog.Info("backfilled receipt headers", "count", updated)
	}
	return nil
}

func receiptExists(db *sql.DB, txID string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM receipts WHERE transaction_id = ?)", txID).Scan(&exists)
	return exists, err
}

func storeReceipt(db *sql.DB, detail *appie.Receipt) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO receipts (transaction_id, date, total_amount)
		VALUES (?, ?, ?)`,
		detail.TransactionID, detail.Date, detail.TotalAmount,
	); err != nil {
		return fmt.Errorf("insert receipt: %w", err)
	}
	if err := insertItems(tx, detail); err != nil {
		return err
	}
	if err := insertDiscounts(tx, detail); err != nil {
		return err
	}
	if err := insertPayments(tx, detail); err != nil {
		return err
	}
	if err := upsertProductPosIDs(tx, detail); err != nil {
		return err
	}
	return tx.Commit()
}

func insertItems(tx *sql.Tx, detail *appie.Receipt) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO items (receipt_id, description, quantity, amount, unit_price, product_id, web_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare item: %w", err)
	}
	defer stmt.Close()

	for _, item := range detail.Items {
		var pid *int
		if item.ProductID != 0 {
			pid = &item.ProductID
		}
		var up *float64
		if item.UnitPrice != 0 {
			up = &item.UnitPrice
		}
		var wid *int
		if item.WebshopID != 0 {
			wid = &item.WebshopID
		}
		if _, err := stmt.Exec(detail.TransactionID, item.Description, item.Quantity, item.Amount, up, pid, wid); err != nil {
			return fmt.Errorf("insert item %q: %w", item.Description, err)
		}
	}
	return nil
}

func insertDiscounts(tx *sql.Tx, detail *appie.Receipt) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO receipt_discounts (receipt_id, name, amount)
		VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare discount: %w", err)
	}
	defer stmt.Close()

	for _, d := range detail.Discounts {
		if _, err := stmt.Exec(detail.TransactionID, d.Name, d.Amount); err != nil {
			return fmt.Errorf("insert discount %q: %w", d.Name, err)
		}
	}
	return nil
}

func insertPayments(tx *sql.Tx, detail *appie.Receipt) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO receipt_payments (receipt_id, method, amount)
		VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare payment: %w", err)
	}
	defer stmt.Close()

	for _, p := range detail.Payments {
		if _, err := stmt.Exec(detail.TransactionID, p.Method, p.Amount); err != nil {
			return fmt.Errorf("insert payment %q: %w", p.Method, err)
		}
	}
	return nil
}

func upsertProductPosIDs(tx *sql.Tx, detail *appie.Receipt) error {
	for _, item := range detail.Items {
		if item.ProductID == 0 || item.WebshopID == 0 {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO products (web_id, pos_id) VALUES (?, ?)
			ON CONFLICT(web_id) DO UPDATE SET pos_id = excluded.pos_id WHERE products.pos_id IS NULL`,
			item.WebshopID, item.ProductID,
		); err != nil {
			return fmt.Errorf("upsert pos_id web_id=%d pos_id=%d: %w", item.WebshopID, item.ProductID, err)
		}
	}
	return nil
}
