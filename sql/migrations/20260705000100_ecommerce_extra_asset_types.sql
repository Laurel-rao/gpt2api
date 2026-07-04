-- +goose Up
ALTER TABLE `ecommerce_tasks`
  ADD COLUMN `extra_asset_types` JSON NULL AFTER `model_asset_id`;

-- +goose Down
ALTER TABLE `ecommerce_tasks`
  DROP COLUMN `extra_asset_types`;
