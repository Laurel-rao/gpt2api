-- +goose Up
ALTER TABLE `ecommerce_assets`
  ADD COLUMN `credit_cost` BIGINT NOT NULL DEFAULT 0 AFTER `progress`;

-- +goose Down
ALTER TABLE `ecommerce_assets`
  DROP COLUMN `credit_cost`;
