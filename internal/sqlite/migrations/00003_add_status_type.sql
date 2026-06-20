-- +goose Up
ALTER TABLE items ADD COLUMN status_type TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE items DROP COLUMN status_type;
