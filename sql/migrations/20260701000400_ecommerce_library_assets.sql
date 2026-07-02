-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `ecommerce_library_assets` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `asset_id`      VARCHAR(64)     NOT NULL,
    `owner_user_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `kind`          VARCHAR(16)     NOT NULL,
    `scope`         VARCHAR(16)     NOT NULL DEFAULT 'private',
    `review_status` VARCHAR(16)    NOT NULL DEFAULT 'draft',
    `name`          VARCHAR(128)    NOT NULL,
    `code`          VARCHAR(128)    NOT NULL DEFAULT '',
    `cover_url`     VARCHAR(1024)   NOT NULL DEFAULT '',
    `gallery_json`  JSON            NULL,
    `tags_json`     JSON            NULL,
    `detail_json`   JSON            NULL,
    `enabled`       TINYINT(1)      NOT NULL DEFAULT 1,
    `review_note`   VARCHAR(500)    NOT NULL DEFAULT '',
    `reviewed_by`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `reviewed_at`   DATETIME        NULL,
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`    DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_asset_id` (`asset_id`),
    KEY `idx_owner_kind_created` (`owner_user_id`, `kind`, `created_at`),
    KEY `idx_kind_scope_review` (`kind`, `scope`, `review_status`),
    KEY `idx_enabled` (`enabled`),
    KEY `idx_code` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商可复用资产库';

CREATE TABLE IF NOT EXISTS `ecommerce_library_asset_files` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `asset_id`      VARCHAR(64)     NOT NULL,
    `file_usage`    VARCHAR(32)     NOT NULL DEFAULT 'gallery',
    `origin_name`   VARCHAR(255)    NOT NULL DEFAULT '',
    `mime`          VARCHAR(64)     NOT NULL DEFAULT '',
    `size_bytes`    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `width`         INT             NOT NULL DEFAULT 0,
    `height`        INT             NOT NULL DEFAULT 0,
    `sha256`        CHAR(64)        NOT NULL,
    `url`           VARCHAR(1024)   NOT NULL DEFAULT '',
    `sort_order`    INT             NOT NULL DEFAULT 0,
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_asset` (`asset_id`, `sort_order`),
    KEY `idx_sha256` (`sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='电商资产库本地文件';

ALTER TABLE `ecommerce_tasks`
  ADD COLUMN `product_asset_id` VARCHAR(64) NOT NULL DEFAULT '' AFTER `reference_images`,
  ADD COLUMN `model_asset_id` VARCHAR(64) NOT NULL DEFAULT '' AFTER `product_asset_id`;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE `ecommerce_tasks`
  DROP COLUMN `model_asset_id`,
  DROP COLUMN `product_asset_id`;

DROP TABLE IF EXISTS `ecommerce_library_asset_files`;
DROP TABLE IF EXISTS `ecommerce_library_assets`;

-- +goose StatementEnd
