-- +goose Up
CREATE TABLE meta (
  key TEXT PRIMARY KEY NOT NULL,
  value TEXT NOT NULL
) STRICT;

CREATE TABLE rate_slots (
  host TEXT PRIMARY KEY NOT NULL,
  next_eligible_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE category_snapshots (
  id TEXT PRIMARY KEY NOT NULL,
  path TEXT NOT NULL UNIQUE,
  label TEXT NOT NULL,
  parent_id TEXT,
  raw_json BLOB NOT NULL,
  observed_at TEXT NOT NULL
) STRICT;

CREATE TABLE filter_snapshots (
  category_id TEXT NOT NULL,
  key TEXT NOT NULL,
  type TEXT NOT NULL,
  search_param TEXT NOT NULL,
  search_style TEXT NOT NULL,
  raw_json BLOB NOT NULL,
  observed_at TEXT NOT NULL,
  proof TEXT NOT NULL,
  PRIMARY KEY (category_id, key)
) STRICT;

CREATE TABLE location_cache (
  query_key TEXT PRIMARY KEY NOT NULL,
  result_json BLOB NOT NULL,
  observed_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
) STRICT;

CREATE TABLE sellers (
  seller_id TEXT PRIMARY KEY NOT NULL,
  folded_name TEXT NOT NULL,
  display_name TEXT NOT NULL,
  public_json BLOB NOT NULL,
  source TEXT NOT NULL,
  completeness TEXT NOT NULL,
  observed_at TEXT NOT NULL
) STRICT;
CREATE INDEX sellers_folded_name_idx ON sellers(folded_name);

CREATE TABLE seller_listings (
  seller_id TEXT NOT NULL REFERENCES sellers(seller_id) ON DELETE CASCADE,
  listing_id TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  url TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  PRIMARY KEY (seller_id, listing_id)
) STRICT;

CREATE TABLE conversations (
  account_hash TEXT NOT NULL,
  conversation_id TEXT NOT NULL,
  listing_id TEXT,
  counterparty TEXT,
  summary_json BLOB NOT NULL,
  remote_fingerprint TEXT NOT NULL,
  remote_updated_at TEXT,
  observed_at TEXT NOT NULL,
  PRIMARY KEY (account_hash, conversation_id)
) STRICT;

CREATE TABLE messages (
  account_hash TEXT NOT NULL,
  conversation_id TEXT NOT NULL,
  message_key TEXT NOT NULL,
  remote_id TEXT,
  direction TEXT NOT NULL,
  received_at TEXT,
  content_digest TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  PRIMARY KEY (account_hash, conversation_id, message_key),
  FOREIGN KEY (account_hash, conversation_id) REFERENCES conversations(account_hash, conversation_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  account_hash TEXT NOT NULL,
  type TEXT NOT NULL,
  conversation_id TEXT,
  listing_id TEXT,
  preview_json BLOB NOT NULL,
  observed_at TEXT NOT NULL
) STRICT;
CREATE INDEX events_account_sequence_idx ON events(account_hash, sequence);

CREATE TABLE cursor_heads (
  account_hash TEXT PRIMARY KEY NOT NULL,
  generation INTEGER NOT NULL,
  acknowledged_sequence INTEGER NOT NULL
) STRICT;

CREATE TABLE confirmation_plans (
  confirmation_id TEXT PRIMARY KEY NOT NULL,
  profile_uuid TEXT NOT NULL,
  account_hash TEXT NOT NULL,
  operation_kind TEXT NOT NULL,
  target_id TEXT NOT NULL,
  message_digest TEXT NOT NULL,
  state TEXT NOT NULL,
  stage TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  outcome TEXT
) STRICT;
CREATE INDEX confirmation_plans_expiry_idx ON confirmation_plans(expires_at);

CREATE TABLE leases (
  name TEXT PRIMARY KEY NOT NULL,
  owner TEXT NOT NULL,
  expires_at_ms INTEGER NOT NULL
) STRICT;
