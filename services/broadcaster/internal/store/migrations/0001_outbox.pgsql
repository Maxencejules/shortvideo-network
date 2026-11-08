-- 0001_outbox: the broadcaster outbox schema that no open owner question
-- (goal 11 Q1-Q4) changes. The runner applies it as svn_broadcaster_owner in
-- one transaction under the migration advisory lock.
--
-- Not here, because an owner answer decides them: signer key classes and key
-- names (Q1), the kind allowlist and forwarded submissions (Q1, Q2),
-- content ownership rows and caller permissions (Q3), and the clearing of
-- stored authorizations (Q4). A later migration adds them.
--
-- Rules every table follows: every text, bytea and jsonb column has a length
-- CHECK; times come from clock_timestamp(); the runtime role
-- svn_broadcaster has no DELETE, TRUNCATE or DDL privilege anywhere; trigger
-- and SQL functions fix search_path and qualify every name. Error messages
-- raised here carry a code only, never an argument value. Custom SQLSTATEs
-- use class SV:
--   SV001 a terminal or immutable row or column was changed
--   SV002 an append-only table was updated, deleted or truncated
--   SV003 the session user is not a registered caller
--   SV004 an argument is outside its bounds
--   SV005 the caller's open-intent cap is reached
--   SV006 the idempotency key is in use with another request digest
--   SV007 the transaction isolation is not READ COMMITTED
--   SV008 a COMMITTED intent and its commitment row disagree
--   SV009 an intent's state and its live attempts disagree
--   SV010 no chain id is bound

CREATE SCHEMA broadcaster;
REVOKE ALL ON SCHEMA broadcaster FROM PUBLIC;
GRANT USAGE ON SCHEMA broadcaster TO svn_broadcaster;

-- Applied migrations; the runner refuses a changed checksum or a gap.
CREATE TABLE broadcaster.schema_migrations (
    version    integer     PRIMARY KEY CHECK (version BETWEEN 1 AND 9999),
    name       text        NOT NULL CHECK (name ~ '^[a-z0-9_]{1,64}$'),
    sha256     bytea       NOT NULL CHECK (octet_length(sha256) = 32),
    applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- The one chain this database serves. Every intent, commitment, stall
-- episode and health row references it, so no row can carry another chain
-- id. Written by the owner role.
CREATE TABLE broadcaster.chain_binding (
    singleton boolean     PRIMARY KEY DEFAULT true CHECK (singleton),
    chain_id  text        NOT NULL UNIQUE CHECK (chain_id ~ '^[!-~]{1,50}$'),
    bound_at  timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Open-intent cap per caller principal (HTTP and SQL paths). enqueue_count
-- counts enqueues; incrementing it takes the row lock that serialises one
-- caller's enqueues, so other callers and the worker never wait on it.
CREATE TABLE broadcaster.caller_limits (
    principal       text    PRIMARY KEY CHECK (principal ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    open_intent_cap integer NOT NULL CHECK (open_intent_cap BETWEEN 1 AND 4096),
    enqueue_count   bigint  NOT NULL DEFAULT 0 CHECK (enqueue_count >= 0)
);

-- SQL-path mapping from a PostgreSQL login role to its caller principal.
-- Written by the owner role only (register_caller).
CREATE TABLE broadcaster.callers (
    role_name text PRIMARY KEY CHECK (role_name ~ '^[a-z_][a-z0-9_]{0,62}$'),
    principal text NOT NULL UNIQUE REFERENCES broadcaster.caller_limits (principal)
);

-- Per-signer sequence state. The row lock serialises sequence allocation;
-- lock_key is the second key of the signer's session advisory lock.
CREATE TABLE broadcaster.signers (
    address         bytea       PRIMARY KEY CHECK (octet_length(address) = 20),
    lock_key        integer     NOT NULL UNIQUE CHECK (lock_key >= 0),
    account_number  bigint      CHECK (account_number >= 0),
    next_sequence   bigint      CHECK (next_sequence >= 0),
    observed_height bigint      NOT NULL DEFAULT 0 CHECK (observed_height >= 0),
    halted_reason   text        CHECK (halted_reason ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    halted_at       timestamptz,
    CHECK ((halted_reason IS NULL) = (halted_at IS NULL)),
    CHECK ((account_number IS NULL) = (next_sequence IS NULL))
);

-- One row per foreign use of a signer sequence (DR-11-15).
CREATE TABLE broadcaster.signer_events (
    id                 bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signer_address     bytea       NOT NULL REFERENCES broadcaster.signers (address),
    sequence           bigint      NOT NULL CHECK (sequence >= 0),
    foreign_tx_hash    bytea       NOT NULL CHECK (octet_length(foreign_tx_hash) = 32),
    detected_at        timestamptz NOT NULL DEFAULT clock_timestamp(),
    cleared_at         timestamptz,
    cleared_by_command text        CHECK (cleared_by_command ~ '^[a-z][a-z0-9-]{0,31}$'),
    CHECK ((cleared_at IS NULL) = (cleared_by_command IS NULL)),
    UNIQUE (signer_address, sequence, foreign_tx_hash)
);

CREATE TABLE broadcaster.intents (
    intent_id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    caller_principal       text        NOT NULL REFERENCES broadcaster.caller_limits (principal),
    idempotency_key        text        NOT NULL CHECK (idempotency_key ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    kind                   text        NOT NULL CHECK (kind ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    entry                  text        NOT NULL CHECK (entry IN ('HTTP', 'SQL')),
    request_digest         bytea       CHECK (octet_length(request_digest) = 32),
    sql_digest             bytea       CHECK (octet_length(sql_digest) = 32),
    request                bytea       CHECK (octet_length(request) BETWEEN 1 AND 65536),
    request_json           jsonb       CHECK (jsonb_typeof(request_json) = 'object' AND octet_length(request_json::text) <= 65536),
    auth_context           jsonb       NOT NULL CHECK (jsonb_typeof(auth_context) = 'object' AND octet_length(auth_context::text) <= 8192),
    validated              boolean     NOT NULL,
    chain_id               text        NOT NULL REFERENCES broadcaster.chain_binding (chain_id),
    signer_address         bytea       REFERENCES broadcaster.signers (address),
    business_id            text        CHECK (business_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    business_attempt       smallint    CHECK (business_attempt BETWEEN 1 AND 8),
    state                  text        NOT NULL CHECK (state IN ('REQUESTED', 'IN_FLIGHT', 'COMMITTED', 'FAILED')),
    phase                  text,
    reason                 text        CHECK (reason ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    failure_code           bigint      CHECK (failure_code BETWEEN 0 AND 4294967295),
    failure_codespace      text        CHECK (failure_codespace ~ '^[!-~]{1,64}$'),
    created_at             timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at             timestamptz NOT NULL,
    next_attempt_at        timestamptz NOT NULL,
    attempt_count          smallint    NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 32),
    final_at               timestamptz,
    committed_tx_hash      bytea       CHECK (octet_length(committed_tx_hash) = 32),
    own_transaction        boolean,
    transitions_recorded   integer     NOT NULL DEFAULT 0 CHECK (transitions_recorded BETWEEN 0 AND 4096),
    transitions_suppressed integer     NOT NULL DEFAULT 0 CHECK (transitions_suppressed >= 0),
    UNIQUE (caller_principal, idempotency_key),
    CHECK (expires_at > created_at),
    CHECK (expires_at - created_at BETWEEN interval '5 seconds' AND interval '7 days'),
    -- An HTTP row is validated at creation and carries no JSON; a SQL row
    -- keeps its JSON and SQL digest.
    CHECK ((entry = 'HTTP' AND request_json IS NULL AND sql_digest IS NULL AND validated)
        OR (entry = 'SQL' AND request_json IS NOT NULL AND sql_digest IS NOT NULL)),
    -- What the worker needs exists exactly when the row is validated.
    CHECK ((validated AND request IS NOT NULL AND request_digest IS NOT NULL AND signer_address IS NOT NULL
                      AND business_id IS NOT NULL AND business_attempt IS NOT NULL)
        OR (NOT validated AND request IS NULL AND request_digest IS NULL AND signer_address IS NULL
                          AND business_id IS NULL AND business_attempt IS NULL)),
    CHECK ((state = 'REQUESTED' AND phase IN ('QUEUED', 'RETRY_WAIT'))
        OR (state = 'IN_FLIGHT' AND validated AND phase IN ('SIGNED', 'ACKNOWLEDGED', 'UNCERTAIN', 'AWAITING_DEADLINE'))
        OR (state IN ('COMMITTED', 'FAILED') AND phase IS NULL)),
    CHECK ((state IN ('COMMITTED', 'FAILED')) = (final_at IS NOT NULL)),
    CHECK ((state = 'COMMITTED') = (committed_tx_hash IS NOT NULL)),
    CHECK ((state = 'COMMITTED') = (own_transaction IS NOT NULL)),
    CHECK (state <> 'FAILED' OR reason IS NOT NULL),
    CHECK (state <> 'COMMITTED' OR reason IS NULL),
    CHECK ((failure_code IS NULL AND failure_codespace IS NULL) OR (state = 'FAILED' AND failure_code IS NOT NULL AND failure_codespace IS NOT NULL))
);
CREATE INDEX intents_open_by_caller ON broadcaster.intents (caller_principal) WHERE state IN ('REQUESTED', 'IN_FLIGHT');
CREATE INDEX intents_due ON broadcaster.intents (signer_address, next_attempt_at) WHERE state = 'REQUESTED' AND validated;
CREATE INDEX intents_unvalidated ON broadcaster.intents (created_at) WHERE state = 'REQUESTED' AND NOT validated;

-- Business ids of an intent; a new one only after reconciliation shows no
-- effect (8.6 step 5).
CREATE TABLE broadcaster.business_attempts (
    intent_id   uuid        NOT NULL REFERENCES broadcaster.intents (intent_id),
    n           smallint    NOT NULL CHECK (n BETWEEN 1 AND 8),
    business_id text        NOT NULL UNIQUE CHECK (business_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    outcome     text        CHECK (outcome ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    PRIMARY KEY (intent_id, n),
    UNIQUE (intent_id, n, business_id)
);
-- The intent's current business attempt must exist with the same id at
-- commit. Deferred, because the two rows reference each other.
ALTER TABLE broadcaster.intents ADD CONSTRAINT intents_current_business_attempt
    FOREIGN KEY (intent_id, business_attempt, business_id)
    REFERENCES broadcaster.business_attempts (intent_id, n, business_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE broadcaster.attempts (
    attempt_id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    intent_id           uuid        NOT NULL REFERENCES broadcaster.intents (intent_id),
    n                   smallint    NOT NULL CHECK (n BETWEEN 1 AND 32),
    business_n          smallint    NOT NULL,
    signer_address      bytea       NOT NULL REFERENCES broadcaster.signers (address),
    account_number      bigint      NOT NULL CHECK (account_number >= 0),
    sequence            bigint      NOT NULL CHECK (sequence >= 0),
    timeout_height      bigint      NOT NULL CHECK (timeout_height > 0),
    gas                 bigint      NOT NULL CHECK (gas BETWEEN 1 AND 20000000),
    tx_bytes            bytea       NOT NULL CHECK (octet_length(tx_bytes) BETWEEN 1 AND 65536),
    tx_hash             bytea       NOT NULL UNIQUE CHECK (tx_hash = sha256(tx_bytes)),
    created_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    phase               text        NOT NULL CHECK (phase IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED', 'COMMITTED_OK',
                                    'COMMITTED_IDEMPOTENT', 'EXECUTION_FAILED', 'INCLUDED_ANTE_FAILED', 'DEAD')),
    sends_started       smallint    NOT NULL DEFAULT 0 CHECK (sends_started >= 0),
    sends_recorded      smallint    NOT NULL DEFAULT 0 CHECK (sends_recorded >= 0),
    unknown_sends       smallint    NOT NULL DEFAULT 0 CHECK (unknown_sends >= 0),
    cache_answers       smallint    NOT NULL DEFAULT 0 CHECK (cache_answers >= 0),
    counted_refusals    smallint    NOT NULL DEFAULT 0 CHECK (counted_refusals >= 0),
    acked               boolean     NOT NULL DEFAULT false,
    last_outcome        text        CHECK (last_outcome IN ('UNKNOWN', 'CACHE', 'NODE_REFUSED', 'MEMPOOL_REJECTED', 'ACK')),
    ack_code            bigint      CHECK (ack_code BETWEEN 0 AND 4294967295),
    ack_codespace       text        CHECK (ack_codespace ~ '^[!-~]{0,64}$'),
    ack_log             text        CHECK (octet_length(ack_log) <= 4096),
    committed_height    bigint      CHECK (committed_height > 0),
    committed_index     integer     CHECK (committed_index >= 0),
    committed_code      bigint      CHECK (committed_code BETWEEN 0 AND 4294967295),
    committed_codespace text        CHECK (committed_codespace ~ '^[!-~]{0,64}$'),
    acc_seq_present     boolean,
    gas_used            bigint      CHECK (gas_used >= 0),
    dead_reason         text        CHECK (dead_reason IN ('D2_REFUSED', 'D3_DEADLINE_PASSED', 'D3_SEQUENCE_USED')),
    dead_at             timestamptz,
    worker_instance     text        NOT NULL CHECK (worker_instance ~ '^[!-~]{1,64}$'),
    FOREIGN KEY (intent_id, business_n) REFERENCES broadcaster.business_attempts (intent_id, n),
    UNIQUE (intent_id, n),
    CHECK (sends_recorded <= sends_started),
    CHECK (unknown_sends + cache_answers + counted_refusals <= sends_recorded),
    CHECK ((committed_height IS NULL) = (committed_index IS NULL)
       AND (committed_height IS NULL) = (committed_code IS NULL)
       AND (committed_height IS NULL) = (committed_codespace IS NULL)
       AND (committed_height IS NULL) = (acc_seq_present IS NULL)
       AND (committed_height IS NULL) = (gas_used IS NULL)),
    CHECK ((dead_at IS NULL) = (dead_reason IS NULL)),
    CHECK ((phase IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED') AND committed_height IS NULL AND dead_at IS NULL)
        OR (phase IN ('COMMITTED_OK', 'COMMITTED_IDEMPOTENT') AND committed_code = 0 AND acc_seq_present AND dead_at IS NULL)
        OR (phase = 'EXECUTION_FAILED' AND committed_code <> 0 AND acc_seq_present AND dead_at IS NULL)
        OR (phase = 'INCLUDED_ANTE_FAILED' AND committed_code <> 0 AND NOT acc_seq_present AND dead_at IS NULL)
        OR (phase = 'DEAD' AND committed_height IS NULL AND dead_at IS NOT NULL)),
    -- (d2) needs at least one started send, every send recorded, and every
    -- recorded outcome a counted refusal (8.5, M1).
    CHECK (dead_reason IS DISTINCT FROM 'D2_REFUSED'
        OR (sends_started >= 1 AND sends_recorded = sends_started AND counted_refusals = sends_recorded))
);
-- At most one live transaction per signer sequence.
CREATE UNIQUE INDEX attempts_one_live_per_sequence ON broadcaster.attempts (signer_address, sequence)
    WHERE dead_at IS NULL AND committed_height IS NULL;
CREATE INDEX attempts_live_by_signer ON broadcaster.attempts (signer_address)
    WHERE dead_at IS NULL AND committed_height IS NULL;

-- Canonical commitment evidence (9.5), written once, only with a COMMITTED
-- intent. height, block_hash, tx_index, code, codespace, gas_*, events and
-- committing_signer describe the first commitment of the effect,
-- first_commit_tx_hash. tx_hash is the transaction this intent's result was
-- read from; it differs from first_commit_tx_hash only for an idempotent
-- replay.
CREATE TABLE broadcaster.commitments (
    intent_id            uuid        PRIMARY KEY REFERENCES broadcaster.intents (intent_id),
    chain_id             text        NOT NULL REFERENCES broadcaster.chain_binding (chain_id),
    business_id          text        NOT NULL CHECK (business_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    height               bigint      NOT NULL CHECK (height > 0),
    block_hash           bytea       NOT NULL CHECK (octet_length(block_hash) = 32),
    tx_hash              bytea       NOT NULL CHECK (octet_length(tx_hash) = 32),
    first_commit_tx_hash bytea       NOT NULL CHECK (octet_length(first_commit_tx_hash) = 32),
    tx_index             integer     NOT NULL CHECK (tx_index >= 0),
    msg_index            integer     NOT NULL CHECK (msg_index = 0),
    code                 bigint      NOT NULL CHECK (code = 0),
    codespace            text        NOT NULL CHECK (codespace ~ '^[!-~]{0,64}$'),
    gas_wanted           bigint      NOT NULL CHECK (gas_wanted >= 0),
    gas_used             bigint      NOT NULL CHECK (gas_used >= 0),
    events               jsonb       NOT NULL CHECK (jsonb_typeof(events) = 'array' AND octet_length(events::text) <= 65536),
    idempotent_replay    boolean     NOT NULL,
    committing_signer    bytea       NOT NULL CHECK (octet_length(committing_signer) = 20),
    recorded_at          timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (idempotent_replay = (tx_hash <> first_commit_tx_hash))
);

-- Append-only history: one row per change of state, phase or observation
-- class, bounded per intent by the caller's limit (intents.transitions_*).
CREATE TABLE broadcaster.transitions (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    intent_id       uuid        NOT NULL REFERENCES broadcaster.intents (intent_id),
    attempt_id      uuid        REFERENCES broadcaster.attempts (attempt_id),
    at              timestamptz NOT NULL DEFAULT clock_timestamp(),
    from_state      text        CHECK (from_state IN ('REQUESTED', 'IN_FLIGHT', 'COMMITTED', 'FAILED')),
    from_phase      text        CHECK (from_phase ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    to_state        text        NOT NULL CHECK (to_state IN ('REQUESTED', 'IN_FLIGHT', 'COMMITTED', 'FAILED')),
    to_phase        text        CHECK (to_phase ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    reason          text        NOT NULL CHECK (reason ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    observation     jsonb       CHECK (octet_length(observation::text) <= 8192),
    worker_instance text        NOT NULL CHECK (worker_instance ~ '^[!-~]{1,64}$')
);
CREATE INDEX transitions_by_intent ON broadcaster.transitions (intent_id, id);

-- Stall episodes written by the observer (section 10); one open at a time.
CREATE TABLE broadcaster.stall_episodes (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chain_id       text        NOT NULL REFERENCES broadcaster.chain_binding (chain_id),
    reason         text        NOT NULL CHECK (reason IN ('HEIGHT_STALLED', 'NODE_UNREACHABLE', 'CATCHING_UP')),
    started_height bigint      NOT NULL CHECK (started_height >= 0),
    started_at     timestamptz NOT NULL DEFAULT clock_timestamp(),
    ended_height   bigint      CHECK (ended_height >= started_height),
    ended_at       timestamptz,
    CHECK ((ended_height IS NULL) = (ended_at IS NULL))
);
CREATE UNIQUE INDEX stall_episodes_one_open ON broadcaster.stall_episodes (chain_id) WHERE ended_at IS NULL;

-- Latest node status seen by the observer; seen_at is the database clock.
CREATE TABLE broadcaster.chain_health (
    chain_id          text        PRIMARY KEY REFERENCES broadcaster.chain_binding (chain_id),
    latest_height     bigint      NOT NULL CHECK (latest_height >= 0),
    latest_time       timestamptz NOT NULL,
    seen_at           timestamptz NOT NULL,
    observer_instance text        NOT NULL CHECK (observer_instance ~ '^[!-~]{1,64}$')
);

-- Trigger functions.

-- T17: a COMMITTED or FAILED intent never changes and is never deleted.
-- Identity columns never change; what validation sets never changes once
-- validated.
CREATE FUNCTION broadcaster.intents_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF OLD.state IN ('COMMITTED', 'FAILED') THEN
        RAISE EXCEPTION 'SVN_INTENT_TERMINAL' USING ERRCODE = 'SV001';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.intent_id <> OLD.intent_id OR NEW.caller_principal <> OLD.caller_principal
       OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.kind <> OLD.kind OR NEW.entry <> OLD.entry
       OR NEW.chain_id <> OLD.chain_id OR NEW.created_at <> OLD.created_at OR NEW.expires_at <> OLD.expires_at
       OR NEW.auth_context IS DISTINCT FROM OLD.auth_context
       OR NEW.sql_digest IS DISTINCT FROM OLD.sql_digest OR NEW.request_json IS DISTINCT FROM OLD.request_json
       OR NEW.attempt_count < OLD.attempt_count
       OR NEW.transitions_recorded < OLD.transitions_recorded
       OR NEW.transitions_suppressed < OLD.transitions_suppressed
       OR (OLD.business_attempt IS NOT NULL AND NEW.business_attempt < OLD.business_attempt) THEN
        RAISE EXCEPTION 'SVN_INTENT_IMMUTABLE_COLUMN' USING ERRCODE = 'SV001';
    END IF;
    IF OLD.validated AND (NOT NEW.validated
       OR NEW.request IS DISTINCT FROM OLD.request OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.signer_address IS DISTINCT FROM OLD.signer_address) THEN
        RAISE EXCEPTION 'SVN_INTENT_VALIDATED_COLUMN' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER intents_terminal_immutable BEFORE UPDATE OR DELETE ON broadcaster.intents
    FOR EACH ROW EXECUTE FUNCTION broadcaster.intents_guard();

-- A terminal attempt never changes (8.5 integrity rule: nothing is
-- rewritten). A live attempt keeps its identity columns, its counters only
-- grow, acked stays true, and its phase only gains knowledge
-- (SIGNED < UNCERTAIN < ACKNOWLEDGED, 9.2).
CREATE FUNCTION broadcaster.attempts_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    old_rank integer;
    new_rank integer;
BEGIN
    IF OLD.phase NOT IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED') THEN
        RAISE EXCEPTION 'SVN_ATTEMPT_TERMINAL' USING ERRCODE = 'SV001';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.attempt_id <> OLD.attempt_id OR NEW.intent_id <> OLD.intent_id OR NEW.n <> OLD.n
       OR NEW.business_n <> OLD.business_n OR NEW.signer_address <> OLD.signer_address
       OR NEW.account_number <> OLD.account_number OR NEW.sequence <> OLD.sequence
       OR NEW.timeout_height <> OLD.timeout_height OR NEW.gas <> OLD.gas OR NEW.tx_bytes <> OLD.tx_bytes
       OR NEW.tx_hash <> OLD.tx_hash OR NEW.created_at <> OLD.created_at
       OR NEW.sends_started < OLD.sends_started OR NEW.sends_recorded < OLD.sends_recorded
       OR NEW.unknown_sends < OLD.unknown_sends OR NEW.cache_answers < OLD.cache_answers
       OR NEW.counted_refusals < OLD.counted_refusals OR (OLD.acked AND NOT NEW.acked) THEN
        RAISE EXCEPTION 'SVN_ATTEMPT_IMMUTABLE_COLUMN' USING ERRCODE = 'SV001';
    END IF;
    old_rank := CASE OLD.phase WHEN 'SIGNED' THEN 1 WHEN 'UNCERTAIN' THEN 2 ELSE 3 END;
    new_rank := CASE NEW.phase WHEN 'SIGNED' THEN 1 WHEN 'UNCERTAIN' THEN 2 WHEN 'ACKNOWLEDGED' THEN 3 ELSE 4 END;
    IF new_rank < old_rank THEN
        RAISE EXCEPTION 'SVN_ATTEMPT_PHASE_LOWERED' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER attempts_terminal_immutable BEFORE UPDATE OR DELETE ON broadcaster.attempts
    FOR EACH ROW EXECUTE FUNCTION broadcaster.attempts_guard();

-- A business attempt keeps its id; its outcome is set once.
CREATE FUNCTION broadcaster.business_attempts_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.intent_id <> OLD.intent_id OR NEW.n <> OLD.n
       OR NEW.business_id <> OLD.business_id OR NEW.created_at <> OLD.created_at
       OR OLD.outcome IS NOT NULL THEN
        RAISE EXCEPTION 'SVN_BUSINESS_ATTEMPT_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER business_attempts_immutable BEFORE UPDATE OR DELETE ON broadcaster.business_attempts
    FOR EACH ROW EXECUTE FUNCTION broadcaster.business_attempts_guard();

-- A signer event is cleared once and otherwise never changes.
CREATE FUNCTION broadcaster.signer_events_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.cleared_at IS NOT NULL OR NEW.id <> OLD.id
       OR NEW.signer_address <> OLD.signer_address OR NEW.sequence <> OLD.sequence
       OR NEW.foreign_tx_hash <> OLD.foreign_tx_hash OR NEW.detected_at <> OLD.detected_at THEN
        RAISE EXCEPTION 'SVN_SIGNER_EVENT_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER signer_events_immutable BEFORE UPDATE OR DELETE ON broadcaster.signer_events
    FOR EACH ROW EXECUTE FUNCTION broadcaster.signer_events_guard();

-- transitions and commitments refuse UPDATE, DELETE and TRUNCATE.
CREATE FUNCTION broadcaster.refuse_change() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    RAISE EXCEPTION 'SVN_APPEND_ONLY' USING ERRCODE = 'SV002';
END
$fn$;
CREATE TRIGGER transitions_append_only BEFORE UPDATE OR DELETE ON broadcaster.transitions
    FOR EACH ROW EXECUTE FUNCTION broadcaster.refuse_change();
CREATE TRIGGER transitions_no_truncate BEFORE TRUNCATE ON broadcaster.transitions
    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_change();
CREATE TRIGGER commitments_write_once BEFORE UPDATE OR DELETE ON broadcaster.commitments
    FOR EACH ROW EXECUTE FUNCTION broadcaster.refuse_change();
CREATE TRIGGER commitments_no_truncate BEFORE TRUNCATE ON broadcaster.commitments
    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_change();

-- At commit: a COMMITTED intent has its commitment row with the same
-- canonical hash, and a commitment row belongs to a COMMITTED intent
-- (9.3: COMMITTED only with evidence read from the chain). The evidence
-- agrees with the intent's own attempts (tx_hash is unique, so at most one
-- attempt carries a hash):
--   - an intent that is not COMMITTED has no attempt resolved COMMITTED_OK
--     or COMMITTED_IDEMPOTENT (a committed success ends the intent in the
--     same transaction, M2);
--   - own_transaction is true exactly when the canonical hash is an attempt
--     of this intent resolved COMMITTED_OK, and no attempt of this intent in
--     another phase (DEAD, a failure, an idempotent replay) carries it
--     (8.5 integrity rule, 9.4);
--   - an idempotent replay's commitment is read from an attempt of this
--     intent resolved COMMITTED_IDEMPOTENT.
-- An attempt changes only together with its intent (check_live_attempts),
-- so the intents and commitments triggers see every change. This check and
-- the next one are SECURITY DEFINER because a deferred trigger runs at
-- COMMIT as the session's user, which for the SQL path is a caller role
-- without SELECT on these tables.
CREATE FUNCTION broadcaster.check_committed_evidence() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_state text;
    v_hash  bytea;
    v_own   boolean;
    v_phase text;
BEGIN
    SELECT i.state, i.committed_tx_hash, i.own_transaction INTO v_state, v_hash, v_own
      FROM broadcaster.intents i WHERE i.intent_id = NEW.intent_id;
    IF TG_TABLE_NAME = 'commitments' THEN
        IF v_state IS DISTINCT FROM 'COMMITTED' OR v_hash IS DISTINCT FROM NEW.first_commit_tx_hash THEN
            RAISE EXCEPTION 'SVN_COMMITMENT_WITHOUT_COMMITTED_INTENT' USING ERRCODE = 'SV008';
        END IF;
        IF NEW.idempotent_replay AND NOT EXISTS (
                SELECT 1 FROM broadcaster.attempts a
                 WHERE a.intent_id = NEW.intent_id AND a.tx_hash = NEW.tx_hash AND a.phase = 'COMMITTED_IDEMPOTENT') THEN
            RAISE EXCEPTION 'SVN_REPLAY_WITHOUT_ATTEMPT' USING ERRCODE = 'SV008';
        END IF;
    ELSIF v_state = 'COMMITTED' THEN
        IF NOT EXISTS (
                SELECT 1 FROM broadcaster.commitments c
                 WHERE c.intent_id = NEW.intent_id AND c.first_commit_tx_hash = v_hash) THEN
            RAISE EXCEPTION 'SVN_COMMITTED_WITHOUT_EVIDENCE' USING ERRCODE = 'SV008';
        END IF;
        SELECT a.phase INTO v_phase FROM broadcaster.attempts a
         WHERE a.intent_id = NEW.intent_id AND a.tx_hash = v_hash;
        IF v_phase IS DISTINCT FROM (CASE WHEN v_own THEN 'COMMITTED_OK' END) THEN
            RAISE EXCEPTION 'SVN_COMMITTED_OWNERSHIP' USING ERRCODE = 'SV008';
        END IF;
    ELSIF EXISTS (
            SELECT 1 FROM broadcaster.attempts a
             WHERE a.intent_id = NEW.intent_id AND a.phase IN ('COMMITTED_OK', 'COMMITTED_IDEMPOTENT')) THEN
        RAISE EXCEPTION 'SVN_COMMITTED_ATTEMPT_OF_OPEN_INTENT' USING ERRCODE = 'SV008';
    END IF;
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER intents_committed_evidence AFTER INSERT OR UPDATE ON broadcaster.intents
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_committed_evidence();
CREATE CONSTRAINT TRIGGER commitments_committed_intent AFTER INSERT ON broadcaster.commitments
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_committed_evidence();

-- At commit: an IN_FLIGHT intent has exactly one live attempt and any other
-- intent has none, so no intent ends, waits or retries with a transaction
-- that could still commit (M2).
CREATE FUNCTION broadcaster.check_live_attempts() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_state text;
    v_live  integer;
BEGIN
    SELECT i.state INTO v_state FROM broadcaster.intents i WHERE i.intent_id = NEW.intent_id;
    SELECT count(*) INTO v_live FROM broadcaster.attempts a
     WHERE a.intent_id = NEW.intent_id AND a.dead_at IS NULL AND a.committed_height IS NULL;
    IF (v_state = 'IN_FLIGHT') <> (v_live = 1) OR v_live > 1 THEN
        RAISE EXCEPTION 'SVN_LIVE_ATTEMPT_MISMATCH' USING ERRCODE = 'SV009';
    END IF;
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER intents_live_attempts AFTER INSERT OR UPDATE ON broadcaster.intents
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_live_attempts();
CREATE CONSTRAINT TRIGGER attempts_live_attempts AFTER INSERT OR UPDATE ON broadcaster.attempts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_live_attempts();

-- SQL path (6.3). A caller service calls this inside its own transaction,
-- next to its own application row, so both commit or neither does. The
-- caller is the session user's registered principal. The function checks
-- syntax and bounds only and stores the request unvalidated; the worker
-- validates it in full before any signing (T2). Raised messages carry a code
-- only. lock_timeout bounds the wait for the caller's own row lock; it is
-- the PROPOSED 2 s (DR-11-10), changed only by a migration.
CREATE FUNCTION broadcaster.enqueue_intent(
    p_kind text, p_request jsonb, p_auth_context jsonb, p_idempotency_key text, p_expires_in interval)
RETURNS TABLE (intent_id uuid, created boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET lock_timeout = '2s'
AS $fn$
#variable_conflict use_column
DECLARE
    v_now       timestamptz := clock_timestamp();
    v_principal text;
    v_chain     text;
    v_cap       integer;
    v_open      integer;
    v_digest    bytea;
    v_id        uuid;
    v_existing  bytea;
    v_kind      text;
    v_auth      jsonb;
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'SVN_ISOLATION' USING ERRCODE = 'SV007';
    END IF;
    SELECT c.principal INTO v_principal FROM broadcaster.callers c WHERE c.role_name = session_user::text;
    IF v_principal IS NULL THEN
        RAISE EXCEPTION 'SVN_CALLER_UNKNOWN' USING ERRCODE = 'SV003';
    END IF;
    IF p_kind IS NULL OR p_kind !~ '^[A-Z][A-Z0-9_]{0,39}$' THEN
        RAISE EXCEPTION 'SVN_BAD_KIND' USING ERRCODE = 'SV004';
    END IF;
    IF p_idempotency_key IS NULL OR p_idempotency_key !~ '^[a-z0-9][a-z0-9._-]{0,63}$' THEN
        RAISE EXCEPTION 'SVN_BAD_IDEMPOTENCY_KEY' USING ERRCODE = 'SV004';
    END IF;
    IF p_request IS NULL OR jsonb_typeof(p_request) <> 'object' OR octet_length(p_request::text) > 65536 THEN
        RAISE EXCEPTION 'SVN_BAD_REQUEST' USING ERRCODE = 'SV004';
    END IF;
    IF p_auth_context IS NULL OR jsonb_typeof(p_auth_context) <> 'object' OR octet_length(p_auth_context::text) > 8192 THEN
        RAISE EXCEPTION 'SVN_BAD_AUTH_CONTEXT' USING ERRCODE = 'SV004';
    END IF;
    IF p_expires_in IS NULL OR p_expires_in < interval '5 seconds' OR p_expires_in > interval '7 days' THEN
        RAISE EXCEPTION 'SVN_BAD_EXPIRY' USING ERRCODE = 'SV004';
    END IF;
    SELECT b.chain_id INTO v_chain FROM broadcaster.chain_binding b;
    IF v_chain IS NULL THEN
        RAISE EXCEPTION 'SVN_CHAIN_UNBOUND' USING ERRCODE = 'SV010';
    END IF;
    -- jsonb text has a fixed key order and no insignificant whitespace.
    v_digest := sha256(convert_to(p_request::text, 'UTF8'));

    UPDATE broadcaster.caller_limits l SET enqueue_count = l.enqueue_count + 1
     WHERE l.principal = v_principal RETURNING l.open_intent_cap INTO v_cap;

    -- A replay repeats the request, the kind and the authorization context
    -- (jsonb equality ignores key order); anything else is a conflict, as on
    -- the HTTP path, whose digest frames all three (5.4).
    SELECT i.intent_id, i.sql_digest, i.kind, i.auth_context INTO v_id, v_existing, v_kind, v_auth
      FROM broadcaster.intents i
     WHERE i.caller_principal = v_principal AND i.idempotency_key = p_idempotency_key;
    IF v_id IS NOT NULL THEN
        IF v_existing IS DISTINCT FROM v_digest OR v_kind IS DISTINCT FROM p_kind OR v_auth IS DISTINCT FROM p_auth_context THEN
            RAISE EXCEPTION 'SVN_IDEMPOTENCY_CONFLICT' USING ERRCODE = 'SV006';
        END IF;
        RETURN QUERY SELECT v_id, false;
        RETURN;
    END IF;

    SELECT count(*) INTO v_open FROM broadcaster.intents i
     WHERE i.caller_principal = v_principal AND i.state IN ('REQUESTED', 'IN_FLIGHT');
    IF v_open >= v_cap THEN
        RAISE EXCEPTION 'SVN_CAP_REACHED' USING ERRCODE = 'SV005';
    END IF;

    INSERT INTO broadcaster.intents AS i (caller_principal, idempotency_key, kind, entry, sql_digest, request_json,
            auth_context, validated, chain_id, state, phase, created_at, expires_at, next_attempt_at,
            transitions_recorded)
    VALUES (v_principal, p_idempotency_key, p_kind, 'SQL', v_digest, p_request, p_auth_context, false, v_chain,
            'REQUESTED', 'QUEUED', v_now, v_now + p_expires_in, v_now, 1)
    RETURNING i.intent_id INTO v_id;
    INSERT INTO broadcaster.transitions (intent_id, to_state, to_phase, reason, worker_instance)
    VALUES (v_id, 'REQUESTED', 'QUEUED', 'CREATED', 'sql-enqueue');
    PERFORM pg_notify('broadcaster_work', '');
    RETURN QUERY SELECT v_id, true;
END
$fn$;
REVOKE ALL ON FUNCTION broadcaster.enqueue_intent(text, jsonb, jsonb, text, interval) FROM PUBLIC;

-- A caller's own intents, with status fields and digests only: no request,
-- no authorization context, no transaction bytes.
CREATE VIEW broadcaster.caller_intents WITH (security_barrier = true) AS
SELECT i.intent_id, i.idempotency_key, i.kind, i.state, i.phase, i.reason, i.failure_code, i.failure_codespace,
       i.sql_digest, i.request_digest, i.business_id, i.attempt_count, i.created_at, i.expires_at, i.final_at,
       i.committed_tx_hash
  FROM broadcaster.intents i
  JOIN broadcaster.callers c ON c.principal = i.caller_principal
 WHERE c.role_name = session_user::text;
REVOKE ALL ON broadcaster.caller_intents FROM PUBLIC;

-- Owner-only: register a caller principal or update its cap, and
-- optionally map a PostgreSQL role to it for the SQL path and grant that
-- role EXECUTE on enqueue_intent and SELECT on caller_intents. The role may
-- not be a superuser, the runtime role or the owner role. The caps of all
-- principals sum to at most 16,384, the most intents one stall transition
-- marks (store.maxStallIntents). Arguments are checked here; the role name
-- reaches GRANT through format(%I).
CREATE FUNCTION broadcaster.register_caller(p_principal text, p_cap integer, p_role text)
RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF p_principal IS NULL OR p_principal !~ '^[a-z0-9][a-z0-9._-]{0,63}$' THEN
        RAISE EXCEPTION 'SVN_BAD_PRINCIPAL' USING ERRCODE = 'SV004';
    END IF;
    IF p_cap IS NULL OR p_cap < 1 OR p_cap > 4096 THEN
        RAISE EXCEPTION 'SVN_BAD_CAP' USING ERRCODE = 'SV004';
    END IF;
    -- Serialise registrations (class 0x53564e00 = 1398164992, key 1; the
    -- migration lock is key 0) so two cannot pass the sum check together;
    -- enqueues do not take this lock.
    PERFORM pg_advisory_xact_lock(1398164992, 1);
    IF (SELECT COALESCE(sum(l.open_intent_cap), 0) FROM broadcaster.caller_limits l WHERE l.principal <> p_principal)
       + p_cap > 16384 THEN
        RAISE EXCEPTION 'SVN_CAP_SUM' USING ERRCODE = 'SV004';
    END IF;
    IF p_role IS NOT NULL AND (p_role !~ '^[a-z_][a-z0-9_]{0,62}$'
       OR p_role IN ('svn_broadcaster', 'svn_broadcaster_owner') OR p_role = current_user::text
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname = p_role AND NOT r.rolsuper)) THEN
        RAISE EXCEPTION 'SVN_BAD_ROLE' USING ERRCODE = 'SV004';
    END IF;
    INSERT INTO broadcaster.caller_limits (principal, open_intent_cap) VALUES (p_principal, p_cap)
    ON CONFLICT (principal) DO UPDATE SET open_intent_cap = EXCLUDED.open_intent_cap;
    IF p_role IS NOT NULL THEN
        INSERT INTO broadcaster.callers (role_name, principal) VALUES (p_role, p_principal);
        EXECUTE format('GRANT USAGE ON SCHEMA broadcaster TO %I', p_role);
        EXECUTE format('GRANT EXECUTE ON FUNCTION broadcaster.enqueue_intent(text, jsonb, jsonb, text, interval) TO %I', p_role);
        EXECUTE format('GRANT SELECT ON broadcaster.caller_intents TO %I', p_role);
    END IF;
END
$fn$;
REVOKE ALL ON FUNCTION broadcaster.register_caller(text, integer, text) FROM PUBLIC;

REVOKE ALL ON FUNCTION broadcaster.intents_guard(), broadcaster.attempts_guard(),
    broadcaster.business_attempts_guard(), broadcaster.signer_events_guard(), broadcaster.refuse_change(),
    broadcaster.check_committed_evidence(), broadcaster.check_live_attempts() FROM PUBLIC;

-- Runtime role privileges: DML without DELETE on the outbox tables, read-only
-- on the owner-written tables, no DDL.
GRANT SELECT ON broadcaster.schema_migrations, broadcaster.chain_binding, broadcaster.callers TO svn_broadcaster;
GRANT SELECT ON broadcaster.caller_limits TO svn_broadcaster;
GRANT UPDATE (enqueue_count) ON broadcaster.caller_limits TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.signers TO svn_broadcaster;
GRANT UPDATE (account_number, next_sequence, observed_height, halted_reason, halted_at) ON broadcaster.signers TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.signer_events TO svn_broadcaster;
GRANT UPDATE (cleared_at, cleared_by_command) ON broadcaster.signer_events TO svn_broadcaster;
GRANT SELECT, INSERT, UPDATE ON broadcaster.intents, broadcaster.attempts TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.business_attempts TO svn_broadcaster;
GRANT UPDATE (outcome) ON broadcaster.business_attempts TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.transitions, broadcaster.commitments TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.stall_episodes TO svn_broadcaster;
GRANT UPDATE (ended_height, ended_at) ON broadcaster.stall_episodes TO svn_broadcaster;
GRANT SELECT, INSERT, UPDATE ON broadcaster.chain_health TO svn_broadcaster;
