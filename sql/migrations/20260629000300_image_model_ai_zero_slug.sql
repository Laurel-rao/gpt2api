-- +goose Up
UPDATE `models`
   SET `upstream_model_slug` = 'gpt-image-2'
 WHERE `type` = 'image'
   AND `slug` = 'gpt-image-2'
   AND `upstream_model_slug` NOT IN ('gpt-image-1', 'gpt-image-1-mini', 'gpt-image-1.5', 'gpt-image-2');

-- +goose Down
UPDATE `models`
   SET `upstream_model_slug` = 'gpt-5-4'
 WHERE `type` = 'image'
   AND `slug` = 'gpt-image-2'
   AND `upstream_model_slug` = 'gpt-image-2';
