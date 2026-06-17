-- Adds shop products that deliver non-refundable load-factor credits.

ALTER TABLE shop_products
    ADD COLUMN IF NOT EXISTS load_factor_credits_per_unit INTEGER NOT NULL DEFAULT 0;

ALTER TABLE shop_orders
    ADD COLUMN IF NOT EXISTS load_factor_credits_awarded INTEGER NOT NULL DEFAULT 0;

ALTER TABLE shop_products
    DROP CONSTRAINT IF EXISTS shop_products_product_type_valid,
    ADD CONSTRAINT shop_products_product_type_valid CHECK (
        product_type IN ('card_key', 'balance_draw', 'points_draw', 'load_factor_credits')
    );

ALTER TABLE shop_products
    DROP CONSTRAINT IF EXISTS shop_products_draw_config_valid,
    ADD CONSTRAINT shop_products_draw_config_valid CHECK (
        (
            product_type IN ('card_key', 'load_factor_credits')
            AND draw_enabled = FALSE
            AND (
                product_type <> 'load_factor_credits'
                OR auto_delivery = TRUE
            )
        )
        OR
        (
            product_type IN ('balance_draw', 'points_draw')
            AND balance_only = TRUE
            AND auto_delivery = TRUE
            AND min_purchase = 1
            AND max_purchase = 1
            AND draw_enabled = TRUE
            AND draw_min_amount > 0
            AND draw_max_amount >= draw_min_amount
            AND draw_guarantee_count > 0
            AND draw_return_rate > 0
            AND ROUND(price * draw_guarantee_count * draw_return_rate * 100) >= ROUND(draw_min_amount * 100) * draw_guarantee_count
            AND ROUND(price * draw_guarantee_count * draw_return_rate * 100) <= ROUND(draw_max_amount * 100) * draw_guarantee_count
        )
    );

ALTER TABLE shop_products
    DROP CONSTRAINT IF EXISTS shop_products_load_factor_credits_valid,
    ADD CONSTRAINT shop_products_load_factor_credits_valid CHECK (
        (
            product_type = 'load_factor_credits'
            AND load_factor_credits_per_unit > 0
        )
        OR
        (
            product_type <> 'load_factor_credits'
            AND load_factor_credits_per_unit = 0
        )
    );

ALTER TABLE shop_orders
    DROP CONSTRAINT IF EXISTS shop_orders_load_factor_credits_awarded_nonnegative,
    ADD CONSTRAINT shop_orders_load_factor_credits_awarded_nonnegative CHECK (
        load_factor_credits_awarded >= 0
    );
