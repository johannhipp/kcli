-- name: GetMeta :one
SELECT value FROM meta WHERE key = ?;

-- name: SetMeta :exec
INSERT INTO meta (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: DeleteMeta :exec
DELETE FROM meta WHERE key = ?;

-- name: GetRateSlot :one
SELECT next_eligible_at_ms FROM rate_slots WHERE host = ?;

-- name: UpsertRateSlot :exec
INSERT INTO rate_slots (host, next_eligible_at_ms) VALUES (?, ?)
ON CONFLICT(host) DO UPDATE SET next_eligible_at_ms = excluded.next_eligible_at_ms;

-- name: GetLease :one
SELECT name, owner, expires_at_ms FROM leases WHERE name = ?;

-- name: DeleteLease :exec
DELETE FROM leases WHERE name = ? AND owner = ?;

-- name: ListSchemaTables :many
SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name;
