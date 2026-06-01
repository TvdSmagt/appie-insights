package syncer

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	appie "github.com/gwillem/appie-go"
)

const maxOrderHistory = 25

func syncOrders(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration, maxOrders int) error {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	fulfillments, err := client.GetOrderHistory(reqCtx, maxOrders)
	cancel()
	if err != nil {
		return fmt.Errorf("GetOrderHistory: %w", err)
	}
	slog.Info("found closed orders from AH", "count", len(fulfillments))

	var synced int
	for _, f := range fulfillments {
		exists, err := orderExists(db, f.OrderID)
		if err != nil {
			return fmt.Errorf("check existing order %d: %w", f.OrderID, err)
		}
		if exists {
			continue
		}

		time.Sleep(delay)

		reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
		detail, err := client.GetOrderHistoryDetail(reqCtx, f.OrderID)
		cancel()
		if err != nil {
			slog.Warn("skip order", "order_id", f.OrderID, "err", err)
			continue
		}

		if err := storeOrder(db, f, detail); err != nil {
			return fmt.Errorf("store order %d: %w", f.OrderID, err)
		}

		synced++
		slog.Info("synced order",
			"order_id", f.OrderID,
			"date", detail.Delivery.Slot.Date,
			"total", f.TotalPrice,
			"items", len(detail.OrderLines))
	}

	slog.Info("order sync complete", "new_orders", synced)
	return nil
}

func orderExists(db *sql.DB, orderID int) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM orders WHERE order_id = ?)", orderID).Scan(&exists)
	return exists, err
}

// storeOrder writes a fulfilled order and its line items in a single transaction.
// summary.TotalPrice is used for the order total because FulfillmentDetail does
// not carry a top-level price field.
func storeOrder(db *sql.DB, summary appie.Fulfillment, detail *appie.FulfillmentDetail) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT OR IGNORE INTO orders
			(order_id, delivery_date, delivery_method, delivery_status,
			 closing_datetime, invoice_id, total_price,
			 address_street, address_number, address_extra, address_postcode, address_city)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		detail.OrderID,
		detail.Delivery.Slot.Date,
		detail.Delivery.Method,
		detail.Delivery.Status,
		detail.ClosingDateTime,
		detail.InvoiceID,
		summary.TotalPrice,
		detail.Delivery.Address.Street,
		detail.Delivery.Address.HouseNumber,
		detail.Delivery.Address.HouseNumberExtra,
		detail.Delivery.Address.PostalCode,
		detail.Delivery.Address.City,
	)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	itemStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO order_items
			(order_id, web_id, title, brand, category, sales_unit_size,
			 quantity, allocated_qty, unit_price, was_price, image_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare order item: %w", err)
	}
	defer itemStmt.Close()

	for _, ol := range detail.OrderLines {
		if ol.Product == nil {
			continue
		}
		var wasPrice *float64
		if ol.Product.WasPrice > 0 {
			wasPrice = &ol.Product.WasPrice
		}
		if _, err := itemStmt.Exec(
			detail.OrderID,
			ol.Product.ID,
			ol.Product.Title,
			ol.Product.Brand,
			ol.Product.Category,
			ol.Product.SalesUnitSize,
			ol.Quantity,
			ol.AllocatedQuantity,
			ol.Product.CurrentPrice,
			wasPrice,
			ol.Product.ImageURL,
		); err != nil {
			return fmt.Errorf("insert order item %q: %w", ol.Product.Title, err)
		}
	}

	return tx.Commit()
}
