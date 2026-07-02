-- +goose Up
INSERT INTO system_settings (`k`, `v`)
VALUES ('videogen.billing_ratio', '10')
ON DUPLICATE KEY UPDATE `v` = `v`;

-- +goose Down
DELETE FROM system_settings
WHERE `k` = 'videogen.billing_ratio';
