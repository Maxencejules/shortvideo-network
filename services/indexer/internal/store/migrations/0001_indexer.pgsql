-- Projection schema for the committed-state indexer.
--
-- These tables are a read model of public coordination data. They hold no
-- video bytes and no personal data. package_bytes is a count. There is no
-- publication-approval column and no token, coin, sale, or mining column.
-- A lease term changes only when a committed LeaseRenewed event is applied.
--
-- Rebuild drops this schema and nothing else. Private social records live
-- in another schema and are not listed here.

CREATE SCHEMA indexer;
REVOKE ALL ON SCHEMA indexer FROM PUBLIC;
GRANT USAGE ON SCHEMA indexer TO svn_indexer;

CREATE TABLE indexer.schema_migrations (
    version    integer     PRIMARY KEY CHECK (version = 1),
    name       text        NOT NULL CHECK (name = 'indexer'),
    sha256     bytea       NOT NULL CHECK (octet_length(sha256) = 32),
    applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE indexer.chain_binding (
    singleton       boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    chain_id        text    NOT NULL UNIQUE CHECK (char_length(chain_id) BETWEEN 1 AND 50),
    decoder_version integer NOT NULL CHECK (decoder_version = 1),
    bound_at        timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE indexer.checkpoint (
    chain_id   text        PRIMARY KEY REFERENCES indexer.chain_binding (chain_id),
    height     bigint      NOT NULL CHECK (height >= 0),
    block_hash bytea       NOT NULL CHECK (octet_length(block_hash) = 32),
    app_hash   bytea       NOT NULL CHECK (octet_length(app_hash) = 32),
    block_time timestamptz NOT NULL
);

CREATE TABLE indexer.events (
    chain_id    text    NOT NULL,
    height      bigint  NOT NULL CHECK (height >= 1),
    tx_hash     bytea   NOT NULL CHECK (octet_length(tx_hash) = 32),
    msg_index   integer NOT NULL CHECK (msg_index BETWEEN -1 AND 1000),
    event_index integer NOT NULL CHECK (event_index BETWEEN 0 AND 9999),
    event_type  text    NOT NULL CHECK (char_length(event_type) BETWEEN 1 AND 80),
    entity      text    NOT NULL CHECK (entity IN ('provider', 'content', 'offer', 'lease', 'policy', 'evidence')),
    entity_id   text    NOT NULL CHECK (char_length(entity_id) BETWEEN 1 AND 160),
    PRIMARY KEY (chain_id, height, tx_hash, msg_index, event_index)
);

CREATE TABLE indexer.providers (
    chain_id        text   NOT NULL,
    provider_id     text   NOT NULL CHECK (char_length(provider_id) BETWEEN 1 AND 64),
    state           text   NOT NULL CHECK (char_length(state) BETWEEN 1 AND 32),
    key_generation  bigint NOT NULL CHECK (key_generation >= 0),
    source_height   bigint NOT NULL,
    source_tx       bytea  NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg      integer NOT NULL,
    source_event    integer NOT NULL,
    source_type     text   NOT NULL,
    PRIMARY KEY (chain_id, provider_id)
);

CREATE TABLE indexer.content (
    chain_id        text    NOT NULL,
    content_id      text    NOT NULL CHECK (char_length(content_id) BETWEEN 1 AND 64),
    content_version bigint  NOT NULL CHECK (content_version >= 1),
    manifest_sha256 text    NOT NULL CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    package_bytes   text    NOT NULL CHECK (package_bytes ~ '^[0-9]{1,20}$'),
    withdrawn       boolean NOT NULL,
    history_erased  boolean NOT NULL,
    source_height   bigint  NOT NULL,
    source_tx       bytea   NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg      integer NOT NULL,
    source_event    integer NOT NULL,
    source_type     text    NOT NULL,
    PRIMARY KEY (chain_id, content_id, content_version)
);

CREATE TABLE indexer.offers (
    chain_id      text NOT NULL,
    offer_id      text NOT NULL CHECK (char_length(offer_id) BETWEEN 1 AND 64),
    provider_id   text NOT NULL CHECK (char_length(provider_id) BETWEEN 1 AND 64),
    content_id    text NOT NULL CHECK (char_length(content_id) BETWEEN 1 AND 64),
    state         text NOT NULL CHECK (state IN ('OPEN', 'CLOSED')),
    source_height bigint NOT NULL,
    source_tx     bytea NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg    integer NOT NULL,
    source_event  integer NOT NULL,
    source_type   text NOT NULL,
    PRIMARY KEY (chain_id, offer_id)
);

CREATE TABLE indexer.leases (
    chain_id        text    NOT NULL,
    lease_id        text    NOT NULL CHECK (char_length(lease_id) BETWEEN 1 AND 64),
    provider_id     text    NOT NULL CHECK (char_length(provider_id) BETWEEN 1 AND 64),
    content_id      text    NOT NULL CHECK (char_length(content_id) BETWEEN 1 AND 64),
    content_version bigint  NOT NULL CHECK (content_version >= 1),
    package_bytes   text    NOT NULL CHECK (package_bytes ~ '^[0-9]{1,20}$'),
    state           text    NOT NULL CHECK (state IN ('ACTIVE', 'RENEWED', 'ENDED', 'EXPIRED')),
    term_version    bigint  NOT NULL CHECK (term_version >= 1),
    deadline_unix   bigint  NOT NULL,
    released_bytes  text    NOT NULL CHECK (released_bytes = '' OR released_bytes ~ '^[0-9]{1,20}$'),
    active          boolean NOT NULL,
    source_height   bigint  NOT NULL,
    source_tx       bytea   NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg      integer NOT NULL,
    source_event    integer NOT NULL,
    source_type     text    NOT NULL,
    PRIMARY KEY (chain_id, lease_id)
);

CREATE TABLE indexer.policy (
    chain_id       text   PRIMARY KEY,
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    parameter_key  text   NOT NULL CHECK (char_length(parameter_key) BETWEEN 0 AND 128),
    source_height  bigint NOT NULL,
    source_tx      bytea  NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg     integer NOT NULL,
    source_event   integer NOT NULL,
    source_type    text   NOT NULL
);

CREATE TABLE indexer.evidence (
    chain_id      text    NOT NULL,
    evidence_id   text    NOT NULL CHECK (char_length(evidence_id) BETWEEN 1 AND 64),
    batch_id      text    NOT NULL CHECK (char_length(batch_id) BETWEEN 1 AND 64),
    kind          text    NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 64),
    attestation   boolean NOT NULL,
    source_height bigint  NOT NULL,
    source_tx     bytea   NOT NULL CHECK (octet_length(source_tx) = 32),
    source_msg    integer NOT NULL,
    source_event  integer NOT NULL,
    source_type   text    NOT NULL,
    PRIMARY KEY (chain_id, evidence_id)
);

CREATE TABLE indexer.reconcile_runs (
    run_id        bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chain_id      text        NOT NULL,
    at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    query_height  bigint      NOT NULL CHECK (query_height >= 0),
    finding_count integer     NOT NULL CHECK (finding_count >= 0),
    status        text        NOT NULL CHECK (status IN ('ok', 'divergent', 'blocked'))
);

CREATE TABLE indexer.findings (
    run_id       bigint NOT NULL REFERENCES indexer.reconcile_runs (run_id),
    kind         text   NOT NULL CHECK (kind ~ '^[a-z0-9_]{1,40}$'),
    object_id    text   NOT NULL CHECK (char_length(object_id) BETWEEN 1 AND 200),
    query_height bigint NOT NULL CHECK (query_height >= 0),
    PRIMARY KEY (run_id, kind, object_id)
);

CREATE TABLE indexer.observer (
    chain_id          text        PRIMARY KEY,
    canonical_height  bigint      NOT NULL CHECK (canonical_height >= 0),
    app_floor         bigint      NOT NULL CHECK (app_floor >= 0),
    block_time        timestamptz NOT NULL,
    observed_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    catching_up       boolean     NOT NULL,
    history_gap       boolean     NOT NULL,
    last_error        text        NOT NULL CHECK (char_length(last_error) <= 80)
);

GRANT SELECT, INSERT, UPDATE ON
    indexer.checkpoint, indexer.events, indexer.providers, indexer.content,
    indexer.offers, indexer.leases, indexer.policy, indexer.evidence,
    indexer.observer
    TO svn_indexer;
GRANT SELECT, INSERT ON indexer.reconcile_runs, indexer.findings TO svn_indexer;
GRANT SELECT ON indexer.chain_binding TO svn_indexer;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA indexer TO svn_indexer;
GRANT SELECT ON indexer.schema_migrations TO svn_indexer;
