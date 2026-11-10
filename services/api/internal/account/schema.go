package account

import (
	"context"
	"database/sql"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS api.accounts (
    account_id text PRIMARY KEY,
    webauthn_id bytea NOT NULL UNIQUE,
    role text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    deleted_at timestamptz,
    CHECK (account_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    CHECK (role IN ('viewer', 'creator', 'operator')),
    CHECK (status IN ('active', 'deleted')),
    CHECK (octet_length(webauthn_id) BETWEEN 16 AND 64)
);

CREATE TABLE IF NOT EXISTS api.invites (
    token_hash text PRIMARY KEY,
    role text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_by text,
    created_at timestamptz NOT NULL,
    CHECK (role IN ('viewer', 'creator', 'operator'))
);

CREATE TABLE IF NOT EXISTS api.passkeys (
    credential_id bytea PRIMARY KEY,
    account_id text NOT NULL REFERENCES api.accounts (account_id),
    record jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS api.sessions (
    token_hash text PRIMARY KEY,
    account_id text NOT NULL REFERENCES api.accounts (account_id),
    csrf_token text NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    confirmed_until timestamptz,
    revoked_at timestamptz
);

CREATE INDEX IF NOT EXISTS sessions_account ON api.sessions (account_id);

CREATE TABLE IF NOT EXISTS api.ceremonies (
    id text PRIMARY KEY,
    purpose text NOT NULL,
    account_id text,
    role text,
    webauthn_id bytea,
    invite_hash text,
    session_data jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (purpose IN ('enroll', 'login', 'confirm'))
);

CREATE TABLE IF NOT EXISTS api.preferences (
    account_id text PRIMARY KEY REFERENCES api.accounts (account_id),
    body jsonb NOT NULL,
    CHECK (octet_length(body::text) <= 2048)
);

CREATE TABLE IF NOT EXISTS api.saved_videos (
    account_id text NOT NULL REFERENCES api.accounts (account_id),
    content_id text NOT NULL,
    PRIMARY KEY (account_id, content_id),
    CHECK (content_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$')
);

CREATE TABLE IF NOT EXISTS api.moderation_notes (
    note_id text PRIMARY KEY,
    subject_account_id text NOT NULL,
    body text NOT NULL,
    CHECK (length(body) <= 4000)
);

CREATE TABLE IF NOT EXISTS api.public_record_refs (
    author_id text NOT NULL,
    record_id text NOT NULL,
    sequence bigint NOT NULL,
    digest text NOT NULL,
    PRIMARY KEY (author_id, record_id, sequence)
);

CREATE TABLE IF NOT EXISTS api.retention (
    account_id text PRIMARY KEY,
    deleted_at timestamptz NOT NULL,
    decision_id text NOT NULL,
    value_version integer NOT NULL,
    history_erased boolean NOT NULL,
    CHECK (decision_id = 'DEC-004'),
    CHECK (value_version = 0),
    CHECK (history_erased = false)
);
`

// Migrate creates the account tables. It does not drop a row.
func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}
