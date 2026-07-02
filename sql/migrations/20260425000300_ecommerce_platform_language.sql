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

UPDATE `ecommerce_platforms`
   SET `language` = CASE LOWER(`language`)
     WHEN 'ja' THEN 'ja-JP'
     WHEN 'jp' THEN 'ja-JP'
     WHEN 'ja-jp' THEN 'ja-JP'
     WHEN 'ko' THEN 'ko-KR'
     WHEN 'kr' THEN 'ko-KR'
     WHEN 'ko-kr' THEN 'ko-KR'
     WHEN 'es' THEN 'es-ES'
     WHEN 'es-es' THEN 'es-ES'
     WHEN 'th' THEN 'th-TH'
     WHEN 'th-th' THEN 'th-TH'
     WHEN 'en' THEN 'en-US'
     WHEN 'en-us' THEN 'en-US'
     WHEN 'zh' THEN 'zh-CN'
     WHEN 'zh-cn' THEN 'zh-CN'
     ELSE `language`
   END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE `ecommerce_platforms`
  DROP COLUMN `language`;

-- +goose StatementEnd
