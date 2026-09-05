-- +goose Up
CREATE INDEX confirmation_plans_lookup_idx ON confirmation_plans(account_hash, operation_kind, target_id, message_digest, state, created_at DESC, confirmation_id DESC);

-- +goose Down
DROP INDEX confirmation_plans_lookup_idx;
