-- +goose Up
UPDATE system_settings
   SET `v` = 'http://43.128.120.182/v1'
 WHERE `k` IN ('imagegen.base_url', 'textgen.base_url')
   AND `v` = 'http://43.134.21.160/v1';

-- +goose Down
UPDATE system_settings
   SET `v` = 'http://43.134.21.160/v1'
 WHERE `k` IN ('imagegen.base_url', 'textgen.base_url')
   AND `v` = 'http://43.128.120.182/v1';
