-- +goose Up
UPDATE system_settings
SET `v` = 'b64_json'
WHERE `k` = 'imagegen.response_format'
  AND (`v` = '' OR `v` = 'url');

-- +goose Down
UPDATE system_settings
SET `v` = 'url'
WHERE `k` = 'imagegen.response_format'
  AND `v` = 'b64_json';
