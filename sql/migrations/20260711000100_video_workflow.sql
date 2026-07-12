-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `video_workflow_templates` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `code`           VARCHAR(64)     NOT NULL,
    `version`        INT UNSIGNED    NOT NULL,
    `name`           VARCHAR(128)    NOT NULL,
    `description`    VARCHAR(500)    NOT NULL DEFAULT '',
    `source_url`     VARCHAR(1024)   NOT NULL DEFAULT '',
    `graph_json`     JSON            NOT NULL,
    `model_settings` JSON            NOT NULL,
    `enabled`        TINYINT(1)      NOT NULL DEFAULT 1,
    `created_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_template_code_version` (`code`, `version`),
    KEY `idx_video_workflow_template_enabled` (`enabled`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流版本化模板';

CREATE TABLE IF NOT EXISTS `video_workflows` (
    `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `workflow_id`      VARCHAR(64)     NOT NULL,
    `user_id`          BIGINT UNSIGNED NOT NULL,
    `template_id`      BIGINT UNSIGNED NULL,
    `template_version` INT UNSIGNED    NOT NULL DEFAULT 0,
    `name`             VARCHAR(128)    NOT NULL,
    `revision`         BIGINT UNSIGNED NOT NULL DEFAULT 1,
    `graph_json`       JSON            NOT NULL,
    `settings_json`    JSON            NOT NULL,
    `created_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`       DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_id` (`workflow_id`),
    KEY `idx_video_workflow_user_updated` (`user_id`, `deleted_at`, `updated_at`),
    KEY `idx_video_workflow_template` (`template_id`, `template_version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户视频工作流草稿';

CREATE TABLE IF NOT EXISTS `video_workflow_runs` (
    `id`                      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `run_id`                  VARCHAR(64)     NOT NULL,
    `workflow_id`             VARCHAR(64)     NOT NULL,
    `user_id`                 BIGINT UNSIGNED NOT NULL,
    `workflow_revision`       BIGINT UNSIGNED NOT NULL,
    `request_id`              VARCHAR(128)    NOT NULL,
    `run_mode`                VARCHAR(32)     NOT NULL DEFAULT 'full',
    `start_node_id`           VARCHAR(128)    NOT NULL DEFAULT '',
    `estimate_token`          VARCHAR(512)    NOT NULL DEFAULT '',
    `estimate_hash`           CHAR(64)        NOT NULL DEFAULT '',
    `estimated_credits`       BIGINT          NOT NULL DEFAULT 0,
    `actual_credits`          BIGINT          NOT NULL DEFAULT 0,
    `output_version_id`       VARCHAR(64)     NOT NULL DEFAULT '',
    `status`                  VARCHAR(40)     NOT NULL DEFAULT 'queued',
    `graph_snapshot`          JSON            NOT NULL,
    `model_settings_snapshot` JSON            NOT NULL,
    `progress`                INT UNSIGNED    NOT NULL DEFAULT 0,
    `error_code`              VARCHAR(64)     NOT NULL DEFAULT '',
    `error_message`           TEXT            NULL,
    `lease_owner`             VARCHAR(128)    NOT NULL DEFAULT '',
    `lease_expires_at`        DATETIME        NULL,
    `heartbeat_at`            DATETIME        NULL,
    `created_at`              DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `started_at`              DATETIME        NULL,
    `finished_at`             DATETIME        NULL,
    `updated_at`              DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_run_id` (`run_id`),
    UNIQUE KEY `uk_video_workflow_run_request` (`user_id`, `request_id`),
    KEY `idx_video_workflow_run_workflow` (`workflow_id`, `created_at`),
    KEY `idx_video_workflow_run_user_status` (`user_id`, `status`, `created_at`),
    KEY `idx_video_workflow_run_recovery` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流不可变运行快照';

CREATE TABLE IF NOT EXISTS `video_workflow_node_runs` (
    `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `node_run_id`       VARCHAR(64)     NOT NULL,
    `run_id`            VARCHAR(64)     NOT NULL,
    `node_id`           VARCHAR(128)    NOT NULL,
    `node_type`         VARCHAR(64)     NOT NULL,
    `input_hash`        CHAR(64)        NOT NULL,
    `status`            VARCHAR(32)     NOT NULL DEFAULT 'queued',
    `progress`          INT UNSIGNED    NOT NULL DEFAULT 0,
    `model_snapshot`    JSON            NULL,
    `upstream_task_id`  VARCHAR(255)    NOT NULL DEFAULT '',
    `provider_state`    JSON            NULL,
    `output_version_id` VARCHAR(64)     NOT NULL DEFAULT '',
    `output_json`       JSON            NULL,
    `credit_cost`       BIGINT          NOT NULL DEFAULT 0,
    `cache_hit`         TINYINT(1)      NOT NULL DEFAULT 0,
    `attempt`           INT UNSIGNED    NOT NULL DEFAULT 1,
    `error_code`        VARCHAR(64)     NOT NULL DEFAULT '',
    `error_message`     TEXT            NULL,
    `created_at`        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `started_at`        DATETIME        NULL,
    `finished_at`       DATETIME        NULL,
    `updated_at`        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_node_run_id` (`node_run_id`),
    UNIQUE KEY `uk_video_workflow_node_attempt` (`run_id`, `node_id`, `attempt`),
    KEY `idx_video_workflow_node_run_status` (`run_id`, `status`),
    KEY `idx_video_workflow_node_cache` (`input_hash`, `node_type`, `status`),
    KEY `idx_video_workflow_node_recovery` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流节点运行记录';

CREATE TABLE IF NOT EXISTS `video_workflow_assets` (
    `id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `asset_id`           VARCHAR(64)     NOT NULL,
    `owner_user_id`      BIGINT UNSIGNED NOT NULL,
    `name`               VARCHAR(255)    NOT NULL,
    `kind`               VARCHAR(32)     NOT NULL,
    `status`             VARCHAR(24)     NOT NULL DEFAULT 'pending',
    `current_version_id` VARCHAR(64)     NOT NULL DEFAULT '',
    `created_at`         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `deleted_at`         DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_asset_id` (`asset_id`),
    KEY `idx_video_workflow_asset_user` (`owner_user_id`, `deleted_at`, `created_at`),
    KEY `idx_video_workflow_asset_kind` (`owner_user_id`, `kind`, `deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流逻辑素材';

CREATE TABLE IF NOT EXISTS `video_workflow_asset_versions` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `version_id`    VARCHAR(64)     NOT NULL,
    `asset_id`      VARCHAR(64)     NOT NULL,
    `owner_user_id` BIGINT UNSIGNED NOT NULL,
    `version`       BIGINT UNSIGNED NOT NULL,
    `status`        VARCHAR(24)     NOT NULL DEFAULT 'ready',
    `mime_type`     VARCHAR(100)    NOT NULL,
    `size_bytes`    BIGINT UNSIGNED NOT NULL,
    `sha256`        CHAR(64)        NOT NULL,
    `storage_key`   VARCHAR(1024)   NOT NULL,
    `file_path`     VARCHAR(1024)   NOT NULL DEFAULT '',
    `parent_version_id` VARCHAR(64) NOT NULL DEFAULT '',
    `source_type`   VARCHAR(32)     NOT NULL DEFAULT 'upload',
    `metadata_json` JSON            NULL,
    `created_by_run_id` VARCHAR(64) NOT NULL DEFAULT '',
    `created_by_node_run_id` VARCHAR(64) NOT NULL DEFAULT '',
    `width`         INT UNSIGNED    NOT NULL DEFAULT 0,
    `height`        INT UNSIGNED    NOT NULL DEFAULT 0,
    `duration_ms`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `input_hash`    CHAR(64)        NOT NULL DEFAULT '',
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `deleted_at`    DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_asset_version_id` (`version_id`),
    UNIQUE KEY `uk_video_workflow_asset_version_number` (`asset_id`, `version`),
    KEY `idx_video_workflow_asset_version_asset` (`asset_id`, `created_at`),
    KEY `idx_video_workflow_asset_version_quota` (`owner_user_id`, `deleted_at`),
    KEY `idx_video_workflow_asset_version_cache` (`owner_user_id`, `input_hash`),
    KEY `idx_video_workflow_asset_version_sha256` (`owner_user_id`, `sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流不可变素材版本';

CREATE TABLE IF NOT EXISTS `video_workflow_asset_refs` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `version_id` VARCHAR(64)     NOT NULL,
    `user_id`    BIGINT UNSIGNED NOT NULL,
    `ref_type`   VARCHAR(32)     NOT NULL,
    `ref_id`     VARCHAR(64)     NOT NULL,
    `node_id`    VARCHAR(128)    NOT NULL DEFAULT '',
    `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `deleted_at` DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_asset_ref` (`version_id`, `ref_type`, `ref_id`, `node_id`),
    KEY `idx_video_workflow_asset_ref_owner` (`user_id`, `ref_type`, `ref_id`),
    KEY `idx_video_workflow_asset_ref_gc` (`version_id`, `deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流素材引用';

CREATE TABLE IF NOT EXISTS `video_workflow_approvals` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `approval_id`    VARCHAR(64)     NOT NULL,
    `run_id`         VARCHAR(64)     NOT NULL,
    `node_run_id`    VARCHAR(64)     NOT NULL,
    `node_id`        VARCHAR(128)    NOT NULL,
    `input_hash`     CHAR(64)        NOT NULL,
    `approval_type`  VARCHAR(32)     NOT NULL,
    `payload_json`   JSON            NULL,
    `status`         VARCHAR(24)     NOT NULL DEFAULT 'pending',
    `decision_note`  VARCHAR(500)    NOT NULL DEFAULT '',
    `expires_at`     DATETIME        NOT NULL,
    `decided_at`     DATETIME        NULL,
    `created_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_approval_id` (`approval_id`),
    UNIQUE KEY `uk_video_workflow_approval_input` (`run_id`, `node_id`, `input_hash`, `approval_type`),
    KEY `idx_video_workflow_approval_run_status` (`run_id`, `status`, `created_at`),
    KEY `idx_video_workflow_approval_expiry` (`status`, `expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流审批等待记录';

CREATE TABLE IF NOT EXISTS `video_workflow_charge_reservations` (
    `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `charge_id`       VARCHAR(64)     NOT NULL,
    `user_id`         BIGINT UNSIGNED NOT NULL,
    `run_id`          VARCHAR(64)     NOT NULL,
    `node_run_id`     VARCHAR(64)     NOT NULL DEFAULT '',
    `idempotency_key` VARCHAR(255)    NOT NULL,
    `amount`          BIGINT          NOT NULL DEFAULT 0,
    `actual_amount`   BIGINT          NOT NULL DEFAULT 0,
    `platform_overage` BIGINT         NOT NULL DEFAULT 0,
    `status`          VARCHAR(16)     NOT NULL DEFAULT 'reserved',
    `billing_ref`     VARCHAR(128)    NOT NULL DEFAULT '',
    `created_at`      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `settled_at`      DATETIME        NULL,
    `refunded_at`     DATETIME        NULL,
    `updated_at`      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_charge_id` (`charge_id`),
    UNIQUE KEY `uk_video_workflow_charge_idempotency` (`idempotency_key`),
    KEY `idx_video_workflow_charge_run` (`run_id`, `node_run_id`),
    KEY `idx_video_workflow_charge_user_status` (`user_id`, `status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流计费预授权';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `video_workflow_charge_reservations`;
DROP TABLE IF EXISTS `video_workflow_approvals`;
DROP TABLE IF EXISTS `video_workflow_asset_refs`;
DROP TABLE IF EXISTS `video_workflow_asset_versions`;
DROP TABLE IF EXISTS `video_workflow_assets`;
DROP TABLE IF EXISTS `video_workflow_node_runs`;
DROP TABLE IF EXISTS `video_workflow_runs`;
DROP TABLE IF EXISTS `video_workflows`;
DROP TABLE IF EXISTS `video_workflow_templates`;
-- +goose StatementEnd
