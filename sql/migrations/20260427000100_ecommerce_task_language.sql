-- +goose Up
ALTER TABLE `ecommerce_tasks`
  ADD COLUMN `language` VARCHAR(16) NOT NULL DEFAULT 'zh-CN' AFTER `style_template_id`;

UPDATE `ecommerce_tasks` t
  JOIN `ecommerce_platforms` p ON p.id = t.platform_id
   SET t.`language` = COALESCE(NULLIF(p.`language`, ''), 'zh-CN')
 WHERE t.`language` = 'zh-CN';
-- +goose Down

ALTER TABLE `ecommerce_tasks`
  DROP COLUMN `language`;
