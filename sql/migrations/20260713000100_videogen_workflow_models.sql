-- +goose Up
-- +goose StatementBegin
INSERT INTO `system_settings` (`k`, `v`) VALUES
  ('videogen.workflow_models', '[{"channel_type":"apiyi_wan27","value":"wan2.7-r2v","label":"API易 Wan2.7 参考图生视频"}]')
ON DUPLICATE KEY UPDATE `v` = `v`;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM `system_settings`
WHERE `k` = 'videogen.workflow_models';
-- +goose StatementEnd
