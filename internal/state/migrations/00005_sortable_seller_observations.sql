-- +goose Up
-- Existing writers used UTC RFC3339Nano: pad fractional seconds without losing
-- nanosecond precision (SQLite date functions would round to milliseconds).
UPDATE sellers SET observed_at = substr(observed_at, 1, 19) || '.' ||
  substr(CASE WHEN length(observed_at) > 20
    THEN substr(observed_at, 21, length(observed_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z';
UPDATE seller_listings SET observed_at = substr(observed_at, 1, 19) || '.' ||
  substr(CASE WHEN length(observed_at) > 20
    THEN substr(observed_at, 21, length(observed_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z';

-- +goose Down
-- Fixed-width timestamps remain valid RFC3339Nano for older readers.
