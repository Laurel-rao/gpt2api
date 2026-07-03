-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES ('videogen.channel_type', 'echoon')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` = 'videogen.channel_type';
