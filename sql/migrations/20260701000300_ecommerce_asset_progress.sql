-- +goose Up
ALTER TABLE `ecommerce_assets`
  ADD COLUMN `progress` INT NOT NULL DEFAULT 0 AFTER `status`;

UPDATE `ecommerce_assets`
   SET `progress` = CASE
     WHEN `status` = 'success' THEN 100
     WHEN `status` = 'running' THEN 10
     WHEN `status` = 'queued' THEN 0
     ELSE 0
   END;
-- +goose Down

ALTER TABLE `ecommerce_assets`
  DROP COLUMN `progress`;
