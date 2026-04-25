-- +goose Up
-- +goose StatementBegin
INSERT INTO system_settings (`k`, `v`, `description`)
VALUES ('site.favicon_url', '', '浏览器标签页图标 URL; 支持 .ico / png / svg，留空则回退到 Logo')
ON DUPLICATE KEY UPDATE `description` = VALUES(`description`);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM system_settings WHERE `k` = 'site.favicon_url';
-- +goose StatementEnd
