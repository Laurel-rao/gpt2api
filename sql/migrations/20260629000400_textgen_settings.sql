-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('textgen.enabled', COALESCE((SELECT `v` FROM (SELECT `v` FROM system_settings WHERE `k`='imagegen.enabled') s), 'false')),
  ('textgen.account', COALESCE((SELECT `v` FROM (SELECT `v` FROM system_settings WHERE `k`='imagegen.account') s), '')),
  ('textgen.api_key', COALESCE((SELECT `v` FROM (SELECT `v` FROM system_settings WHERE `k`='imagegen.api_key') s), '')),
  ('textgen.base_url', COALESCE((SELECT `v` FROM (SELECT `v` FROM system_settings WHERE `k`='imagegen.base_url') s), 'http://43.128.120.182/v1')),
  ('textgen.model', 'gpt-5.4'),
  ('textgen.timeout_sec', '120')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'textgen.enabled',
  'textgen.account',
  'textgen.api_key',
  'textgen.base_url',
  'textgen.model',
  'textgen.timeout_sec'
);
