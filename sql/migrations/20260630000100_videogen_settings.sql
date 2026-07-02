-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES
  ('videogen.enabled', 'false'),
  ('videogen.account', ''),
  ('videogen.api_key', ''),
  ('videogen.base_url', 'http://app.echoon.top/api/v1'),
  ('videogen.model', '0e37fa2d-72b3-483a-81b4-ad595cd147c7'),
  ('videogen.timeout_sec', '900'),
  ('videogen.duration_sec', '5'),
  ('videogen.aspect_ratio', '16:9'),
  ('videogen.resolution', '720p'),
  ('videogen.generate_audio', 'false')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` IN (
  'videogen.enabled',
  'videogen.account',
  'videogen.api_key',
  'videogen.base_url',
  'videogen.model',
  'videogen.timeout_sec',
  'videogen.duration_sec',
  'videogen.aspect_ratio',
  'videogen.resolution',
  'videogen.generate_audio'
);
