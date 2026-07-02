-- +goose Up
ALTER TABLE `ecommerce_tasks`
    ADD COLUMN `deleted_at` DATETIME NULL AFTER `finished_at`,
    ADD COLUMN `deleted_by` BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER `deleted_at`,
    ADD KEY `idx_user_deleted_created` (`user_id`, `deleted_at`, `created_at`);

-- +goose Down
ALTER TABLE `ecommerce_tasks`
    DROP KEY `idx_user_deleted_created`,
    DROP COLUMN `deleted_by`,
    DROP COLUMN `deleted_at`;
