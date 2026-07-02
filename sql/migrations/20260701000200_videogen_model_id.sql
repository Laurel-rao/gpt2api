-- +goose Up
UPDATE system_settings
   SET v = '0e37fa2d-72b3-483a-81b4-ad595cd147c7'
 WHERE k = 'videogen.model'
   AND v IN ('Seedance 2.0', 'Seedance-2.0-D-V', '');

-- +goose Down
UPDATE system_settings
   SET v = 'Seedance-2.0-D-V'
 WHERE k = 'videogen.model'
   AND v = '0e37fa2d-72b3-483a-81b4-ad595cd147c7';
