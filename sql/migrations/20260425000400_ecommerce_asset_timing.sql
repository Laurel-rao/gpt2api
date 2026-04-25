-- +goose Up
-- +goose StatementBegin

ALTER TABLE `ecommerce_assets`
  ADD COLUMN `started_at` DATETIME NULL AFTER `created_at`,
  ADD COLUMN `finished_at` DATETIME NULL AFTER `started_at`;

UPDATE `ecommerce_assets`
   SET `started_at` = COALESCE(`started_at`, `updated_at`),
       `finished_at` = COALESCE(`finished_at`, `updated_at`)
 WHERE `status` IN ('success', 'failed');

UPDATE `ecommerce_assets`
   SET `started_at` = COALESCE(`started_at`, `updated_at`)
 WHERE `status` = 'running';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE `ecommerce_assets`
  DROP COLUMN `finished_at`,
  DROP COLUMN `started_at`;

-- +goose StatementEnd
