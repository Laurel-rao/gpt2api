-- +goose Up
-- +goose StatementBegin

-- 唯一索引建立前先把现有工作流流水灌入临时主键表；存在重复键时迁移
-- 立即失败，避免在已有账实不一致的情况下静默启用幂等约束。
DROP TEMPORARY TABLE IF EXISTS `video_workflow_credit_idempotency_preflight`;

CREATE TEMPORARY TABLE `video_workflow_credit_idempotency_preflight` (
    `idempotency_key` VARCHAR(191) NOT NULL,
    PRIMARY KEY (`idempotency_key`)
) ENGINE=InnoDB;

INSERT INTO `video_workflow_credit_idempotency_preflight` (`idempotency_key`)
SELECT CONCAT(CAST(`user_id` AS CHAR), ':', `type`, ':', `ref_id`)
  FROM `credit_transactions`
 WHERE `ref_id` LIKE 'video-workflow:%';

DROP TEMPORARY TABLE `video_workflow_credit_idempotency_preflight`;

SET @vw_add_idempotency_column = IF(
    EXISTS(
        SELECT 1 FROM information_schema.columns
         WHERE table_schema=DATABASE() AND table_name='credit_transactions'
           AND column_name='video_workflow_idempotency_key'
    ),
    'SELECT 1',
    'ALTER TABLE `credit_transactions` ADD COLUMN `video_workflow_idempotency_key` VARCHAR(191) GENERATED ALWAYS AS (CASE WHEN `ref_id` LIKE ''video-workflow:%'' THEN CONCAT(CAST(`user_id` AS CHAR), '':'', `type`, '':'', `ref_id`) ELSE NULL END) STORED'
);
PREPARE vw_stmt FROM @vw_add_idempotency_column;
EXECUTE vw_stmt;
DEALLOCATE PREPARE vw_stmt;

SET @vw_add_idempotency_index = IF(
    EXISTS(
        SELECT 1 FROM information_schema.statistics
         WHERE table_schema=DATABASE() AND table_name='credit_transactions'
           AND index_name='uk_credit_video_workflow_idempotency'
    ),
    'SELECT 1',
    'ALTER TABLE `credit_transactions` ADD UNIQUE KEY `uk_credit_video_workflow_idempotency` (`video_workflow_idempotency_key`)'
);
PREPARE vw_stmt FROM @vw_add_idempotency_index;
EXECUTE vw_stmt;
DEALLOCATE PREPARE vw_stmt;

CREATE TABLE IF NOT EXISTS `video_workflow_runtime_slots` (
    `slot_kind`       VARCHAR(24)  NOT NULL,
    `slot_index`      INT UNSIGNED NOT NULL,
    `run_id`          VARCHAR(64)  NOT NULL DEFAULT '',
    `node_run_id`     VARCHAR(64)  NOT NULL DEFAULT '',
    `lease_owner`     VARCHAR(191) NOT NULL DEFAULT '',
    `lease_token`     VARCHAR(255) NULL,
    `lease_expires_at` DATETIME(6) NULL,
    `heartbeat_at`    DATETIME(6) NULL,
    `created_at`      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at`      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`slot_kind`, `slot_index`),
    UNIQUE KEY `uk_video_workflow_runtime_slot_token` (`lease_token`),
    KEY `idx_video_workflow_runtime_slot_expiry` (`slot_kind`, `lease_expires_at`),
    KEY `idx_video_workflow_runtime_slot_run` (`run_id`, `node_run_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流跨实例执行槽位租约';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS `video_workflow_runtime_slots`;

SET @vw_drop_idempotency_index = IF(
    EXISTS(
        SELECT 1 FROM information_schema.statistics
         WHERE table_schema=DATABASE() AND table_name='credit_transactions'
           AND index_name='uk_credit_video_workflow_idempotency'
    ),
    'ALTER TABLE `credit_transactions` DROP INDEX `uk_credit_video_workflow_idempotency`',
    'SELECT 1'
);
PREPARE vw_stmt FROM @vw_drop_idempotency_index;
EXECUTE vw_stmt;
DEALLOCATE PREPARE vw_stmt;

SET @vw_drop_idempotency_column = IF(
    EXISTS(
        SELECT 1 FROM information_schema.columns
         WHERE table_schema=DATABASE() AND table_name='credit_transactions'
           AND column_name='video_workflow_idempotency_key'
    ),
    'ALTER TABLE `credit_transactions` DROP COLUMN `video_workflow_idempotency_key`',
    'SELECT 1'
);
PREPARE vw_stmt FROM @vw_drop_idempotency_column;
EXECUTE vw_stmt;
DEALLOCATE PREPARE vw_stmt;

-- +goose StatementEnd
