-- A shop's shipping rates for storefront orders: one flat fee per order, optionally free once the
-- order's subtotal reaches an amount ("ส่งฟรีเมื่อซื้อครบ ฿500"). Amounts in satang, like everywhere.
-- Existing shops keep shipping free (fee 0), as before this migration.
ALTER TABLE tenants
    ADD COLUMN shipping_fee_satang       bigint NOT NULL DEFAULT 0,
    ADD COLUMN free_shipping_from_satang bigint,
    -- Up to ฿10,000: a typo like an extra zero should not reach customers unnoticed.
    ADD CONSTRAINT tenants_shipping_fee_valid
        CHECK (shipping_fee_satang BETWEEN 0 AND 1000000),
    ADD CONSTRAINT tenants_free_shipping_from_valid
        CHECK (free_shipping_from_satang IS NULL OR free_shipping_from_satang BETWEEN 1 AND 1000000000);
