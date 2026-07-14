-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS `video_workflow_revisions` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `workflow_id`   VARCHAR(64)     NOT NULL,
    `user_id`       BIGINT UNSIGNED NOT NULL,
    `revision`      BIGINT UNSIGNED NOT NULL,
    `name`          VARCHAR(128)    NOT NULL,
    `graph_json`    JSON            NOT NULL,
    `settings_json` JSON            NOT NULL,
    `node_count`    INT UNSIGNED    NOT NULL DEFAULT 0,
    `edge_count`    INT UNSIGNED    NOT NULL DEFAULT 0,
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_video_workflow_revision` (`workflow_id`, `revision`),
    KEY `idx_video_workflow_revision_user` (`user_id`, `workflow_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='视频工作流画布修订快照';

INSERT INTO `video_workflow_revisions`
  (`workflow_id`, `user_id`, `revision`, `name`, `graph_json`, `settings_json`, `node_count`, `edge_count`, `created_at`)
SELECT
  w.`workflow_id`,
  w.`user_id`,
  w.`revision`,
  w.`name`,
  w.`graph_json`,
  w.`settings_json`,
  COALESCE(JSON_LENGTH(JSON_EXTRACT(w.`graph_json`, '$.nodes')), 0),
  COALESCE(JSON_LENGTH(JSON_EXTRACT(w.`graph_json`, '$.edges')), 0),
  w.`updated_at`
FROM `video_workflows` w
WHERE w.`deleted_at` IS NULL
ON DUPLICATE KEY UPDATE
  `name`=VALUES(`name`),
  `graph_json`=VALUES(`graph_json`),
  `settings_json`=VALUES(`settings_json`),
  `node_count`=VALUES(`node_count`),
  `edge_count`=VALUES(`edge_count`);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `video_workflow_revisions`;
-- +goose StatementEnd
