-- Checkout grouping + auto-renew on order lines
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS checkout_id UUID,
    ADD COLUMN IF NOT EXISTS auto_renew BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS orders_checkout_id_idx ON orders(checkout_id);
CREATE INDEX IF NOT EXISTS orders_razorpay_order_id_idx ON orders(razorpay_order_id);
