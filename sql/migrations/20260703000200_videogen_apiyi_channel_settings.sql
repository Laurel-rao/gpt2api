-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('videogen.apiyi_seedance2.account', 'API易 Seedance 2.0'),
  ('videogen.apiyi_seedance2.api_key', ''),
  ('videogen.apiyi_seedance2.base_url', 'https://api.apiyi.com'),
  ('videogen.apiyi_seedance2.model', 'doubao-seedance-2-0-fast-260128')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'videogen.apiyi_seedance2.account',
  'videogen.apiyi_seedance2.api_key',
  'videogen.apiyi_seedance2.base_url',
  'videogen.apiyi_seedance2.model'
);
