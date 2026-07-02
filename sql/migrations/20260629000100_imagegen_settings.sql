-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('imagegen.enabled', 'false'),
  ('imagegen.account', ''),
  ('imagegen.api_key', ''),
  ('imagegen.base_url', 'http://43.134.21.160/v1'),
  ('imagegen.quality', 'low'),
  ('imagegen.background', 'auto'),
  ('imagegen.output_format', 'png'),
  ('imagegen.response_format', 'b64_json'),
  ('imagegen.timeout_sec', '420')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'imagegen.enabled',
  'imagegen.account',
  'imagegen.api_key',
  'imagegen.base_url',
  'imagegen.quality',
  'imagegen.background',
  'imagegen.output_format',
  'imagegen.response_format',
  'imagegen.timeout_sec'
);
