-- +goose Up
-- +goose StatementBegin

ALTER TABLE `ecommerce_platforms`
  ADD COLUMN `language` VARCHAR(16) NOT NULL DEFAULT 'zh-CN' AFTER `name`;

UPDATE `ecommerce_platforms`
   SET `language` = COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(`field_schema`, '$.locale')), 'null'), 'zh-CN')
 WHERE `field_schema` IS NOT NULL;

UPDATE `ecommerce_platforms`
   SET `language` = 'en-US'
 WHERE `code` IN ('amazon', 'shopee', 'shopify');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE `ecommerce_platforms`
  DROP COLUMN `language`;

-- +goose StatementEnd
