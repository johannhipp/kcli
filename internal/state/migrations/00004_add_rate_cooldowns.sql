-- +goose Up
CREATE TABLE rate_cooldowns (
  host TEXT PRIMARY KEY NOT NULL,
  not_before_ms INTEGER NOT NULL
) STRICT;

-- +goose Down
DROP TABLE rate_cooldowns;
