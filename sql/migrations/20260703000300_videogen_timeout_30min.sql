-- +goose Up
UPDATE system_settings
SET v = '1800'
WHERE k = 'videogen.timeout_sec'
  AND (v = '' OR v = '900');

-- +goose Down
UPDATE system_settings
SET v = '900'
WHERE k = 'videogen.timeout_sec'
  AND v = '1800';
