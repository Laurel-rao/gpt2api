-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('videogen.apiyi_wan27.account', 'API易 Wan2.7'),
  ('videogen.apiyi_wan27.api_key', ''),
  ('videogen.apiyi_wan27.base_url', 'https://api.apiyi.com'),
  ('videogen.apiyi_wan27.model', 'wan2.7-r2v')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'videogen.apiyi_wan27.account',
  'videogen.apiyi_wan27.api_key',
  'videogen.apiyi_wan27.base_url',
  'videogen.apiyi_wan27.model'
);
