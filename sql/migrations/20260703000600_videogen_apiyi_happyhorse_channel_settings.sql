-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('videogen.apiyi_happyhorse.account', 'API易 HappyHorse'),
  ('videogen.apiyi_happyhorse.api_key', ''),
  ('videogen.apiyi_happyhorse.base_url', 'https://api.apiyi.com'),
  ('videogen.apiyi_happyhorse.model', 'happyhorse-1.0-r2v')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'videogen.apiyi_happyhorse.account',
  'videogen.apiyi_happyhorse.api_key',
  'videogen.apiyi_happyhorse.base_url',
  'videogen.apiyi_happyhorse.model'
);
