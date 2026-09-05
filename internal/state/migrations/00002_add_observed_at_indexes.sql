-- +goose Up
CREATE INDEX events_account_observed_idx ON events(account_hash, observed_at);
CREATE INDEX sellers_observed_idx ON sellers(observed_at, seller_id);

-- +goose Down
DROP INDEX sellers_observed_idx;
DROP INDEX events_account_observed_idx;
