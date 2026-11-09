-- 0002_answers: the outbox schema that the owner's answers to goal 11 Q1-Q4
-- decide (answered 2026-11-08T07:44Z): Q1 "Both keys, separate signer
-- (Recommended)", Q2 "Relay + verbatim forward (Recommended)", Q3
-- "Operator-registered intent keys (Recommended)", Q4 "Minimal + clear
-- unsent (Recommended)". The answers approve these choices, not this file:
-- every number and the boundary document stay PROPOSED.
--
--   Q1: the request kinds and the class of account that signs each; a key
--       signer row binds an address to its class and key name.
--   Q2: FORWARD_PROVIDER_TX carries a provider-signed transaction verbatim.
--       The provider account it uses is a FORWARD_SOURCE signer row: it
--       holds no key here and no sequence is allocated for it, but the
--       validated-row CHECK of 0001 (a validated intent names a registered
--       signer) keeps its meaning, "the account whose sequence the
--       transaction uses is registered". forwarded_txs keeps the decoded,
--       validated fields; the bytes are the intent's request.
--   Q3: content_owners: a content id belongs to a caller and principal only
--       after its registration committed under our service account and the
--       chain showed that host (plan 5.7). The intent-key registry is an
--       operator file (plan 6.1, 7.4); no table holds it.
--   Q4: when an intent ends FAILED and no attempt of it ever started a send,
--       its request (inner signatures, hosting intent, forwarded bytes), its
--       SQL-path request JSON, its authorization context and its attempts'
--       transaction bytes are cleared; request_digest, sql_digest, tx_hash
--       and a new auth_context_digest are kept. The terminal-row triggers
--       accept exactly that clearing and nothing else, and the commit-time
--       check refuses a FAILED never-sent intent that was not cleared.
--
-- A database that already holds signer rows, validated intents or intents
-- of a kind outside the list is refused (their class, object or kind cannot
-- be derived); no deployment of 0001 alone exists.
--
-- New SQLSTATEs (class SV, see 0001):
--   SV011 a FAILED intent that never started a send keeps its authorizations
--   SV012 the signer's class does not fit the intent's kind, or an attempt
--         names another signer than its intent
--   SV013 a forwarded transaction and its intent or attempt disagree
--   SV014 a content owner row and its register intent disagree

-- Q1, Q2: request kinds. signer_class is the class of the account whose
-- sequence the transaction uses; object_type is the chain object the intent
-- acts on (NULL for FORWARD_PROVIDER_TX, where the message type decides).
CREATE TABLE broadcaster.kinds (
    kind         text PRIMARY KEY CHECK (kind ~ '^[A-Z][A-Z0-9_]{0,39}$'),
    signer_class text NOT NULL CHECK (signer_class IN ('SERVICE', 'BROADCASTER', 'FORWARD_SOURCE')),
    object_type  text CHECK (object_type IN ('CONTENT', 'OFFER', 'LEASE')),
    CHECK ((signer_class = 'FORWARD_SOURCE') = (object_type IS NULL))
);
INSERT INTO broadcaster.kinds (kind, signer_class, object_type) VALUES
    ('REGISTER_CONTENT', 'SERVICE', 'CONTENT'),
    ('WITHDRAW_CONTENT', 'SERVICE', 'CONTENT'),
    ('OFFER_LEASE', 'SERVICE', 'OFFER'),
    ('RELAY_ACCEPT_LEASE', 'BROADCASTER', 'OFFER'),
    ('RELAY_RENEW_LEASE', 'BROADCASTER', 'LEASE'),
    ('RELAY_END_LEASE', 'BROADCASTER', 'LEASE'),
    ('FORWARD_PROVIDER_TX', 'FORWARD_SOURCE', NULL);
-- kinds, and the TRUNCATE of forwarded_txs and content_owners, refuse the
-- statement (rows of these tables are never removed).
CREATE FUNCTION broadcaster.refuse_statement() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    RAISE EXCEPTION 'SVN_TABLE_FIXED' USING ERRCODE = 'SV001';
END
$fn$;
CREATE TRIGGER kinds_fixed BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON broadcaster.kinds
    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_statement();

-- Q1: a key signer (SERVICE or BROADCASTER) has a key name and an advisory
-- lock key; a FORWARD_SOURCE row (Q2) has neither, is never synced and is
-- never halted.
ALTER TABLE broadcaster.signers
    ADD COLUMN class text CHECK (class IN ('SERVICE', 'BROADCASTER', 'FORWARD_SOURCE')),
    ADD COLUMN key_name text UNIQUE CHECK (key_name ~ '^[a-z][a-z0-9_-]{0,31}$'),
    ALTER COLUMN lock_key DROP NOT NULL;
ALTER TABLE broadcaster.signers ALTER COLUMN class SET NOT NULL;
ALTER TABLE broadcaster.signers ADD CONSTRAINT signers_class_binding CHECK (
    (class IN ('SERVICE', 'BROADCASTER') AND key_name IS NOT NULL AND lock_key IS NOT NULL)
 OR (class = 'FORWARD_SOURCE' AND key_name IS NULL AND lock_key IS NULL AND account_number IS NULL
     AND next_sequence IS NULL AND halted_reason IS NULL));

-- Intents: the validated object, the Q4 clearing columns, the kind list.
ALTER TABLE broadcaster.intents
    ADD COLUMN object_type text CHECK (object_type IN ('CONTENT', 'OFFER', 'LEASE')),
    ADD COLUMN object_id text CHECK (object_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    ADD COLUMN object_version bigint CHECK (object_version BETWEEN 0 AND 9223372036854775807),
    ADD COLUMN auth_context_digest bytea CHECK (octet_length(auth_context_digest) = 32),
    ADD COLUMN cleared_at timestamptz,
    ALTER COLUMN auth_context DROP NOT NULL,
    ADD CONSTRAINT intents_kind_fkey FOREIGN KEY (kind) REFERENCES broadcaster.kinds (kind);
CREATE INDEX intents_by_object ON broadcaster.intents (kind, object_id) WHERE validated;

-- Replace the two 0001 CHECKs that a cleared row would break; each is found
-- by its definition and must match exactly once.
DO $do$
DECLARE
    v_name  text;
    v_count integer;
    v_pat   text;
BEGIN
    FOREACH v_pat IN ARRAY ARRAY[
        '%(entry = ''HTTP''::text) AND (request_json IS NULL)%',
        '%(validated AND (request IS NOT NULL)%'] LOOP
        SELECT count(*), min(c.conname) INTO v_count, v_name FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'broadcaster.intents'::regclass AND c.contype = 'c'
           AND pg_catalog.pg_get_constraintdef(c.oid) LIKE v_pat;
        IF v_count <> 1 THEN
            RAISE EXCEPTION 'SVN_0002_CONSTRAINT_NOT_FOUND' USING ERRCODE = 'SV004';
        END IF;
        EXECUTE format('ALTER TABLE broadcaster.intents DROP CONSTRAINT %I', v_name);
    END LOOP;
END
$do$;
ALTER TABLE broadcaster.intents
    -- An HTTP row is validated at creation and carries no JSON; a SQL row
    -- keeps its SQL digest, and its JSON until a Q4 clearing.
    ADD CONSTRAINT intents_entry_shape CHECK (
        (entry = 'HTTP' AND request_json IS NULL AND sql_digest IS NULL AND validated)
     OR (entry = 'SQL' AND (request_json IS NOT NULL OR cleared_at IS NOT NULL) AND sql_digest IS NOT NULL)),
    -- What the worker needs exists exactly when the row is validated; only a
    -- Q4 clearing removes the request.
    ADD CONSTRAINT intents_validated_shape CHECK (
        (validated AND (request IS NOT NULL OR cleared_at IS NOT NULL) AND request_digest IS NOT NULL
                   AND signer_address IS NOT NULL AND business_id IS NOT NULL AND business_attempt IS NOT NULL
                   AND object_type IS NOT NULL AND object_id IS NOT NULL
                   AND (object_type = 'CONTENT') = (object_version IS NOT NULL))
     OR (NOT validated AND request IS NULL AND request_digest IS NULL AND signer_address IS NULL
                       AND business_id IS NULL AND business_attempt IS NULL
                       AND object_type IS NULL AND object_id IS NULL AND object_version IS NULL)),
    -- Q4: a row is cleared only when FAILED, and then holds no request, no
    -- request JSON and no authorization context, only their digests.
    ADD CONSTRAINT intents_cleared_shape CHECK (
        (cleared_at IS NULL AND auth_context IS NOT NULL AND auth_context_digest IS NULL)
     OR (cleared_at IS NOT NULL AND state = 'FAILED' AND request IS NULL AND request_json IS NULL
         AND auth_context IS NULL AND auth_context_digest IS NOT NULL));

-- Attempts: a live attempt keeps its bytes; only a never-sent attempt is
-- ever cleared (Q4).
ALTER TABLE broadcaster.attempts
    ALTER COLUMN tx_bytes DROP NOT NULL,
    ADD CONSTRAINT attempts_bytes_kept CHECK (
        tx_bytes IS NOT NULL OR (sends_started = 0 AND phase NOT IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED')));

-- Business attempts: drawn ids stay globally unique; a forwarded id is the
-- provider's choice (it may repeat after a dead forward is re-signed).
ALTER TABLE broadcaster.business_attempts
    ADD COLUMN source text NOT NULL DEFAULT 'DRAWN' CHECK (source IN ('DRAWN', 'FORWARDED'));
DO $do$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                    WHERE c.conrelid = 'broadcaster.business_attempts'::regclass
                      AND c.conname = 'business_attempts_business_id_key' AND c.contype = 'u') THEN
        RAISE EXCEPTION 'SVN_0002_CONSTRAINT_NOT_FOUND' USING ERRCODE = 'SV004';
    END IF;
END
$do$;
ALTER TABLE broadcaster.business_attempts DROP CONSTRAINT business_attempts_business_id_key;
CREATE UNIQUE INDEX business_attempts_drawn_id ON broadcaster.business_attempts (business_id) WHERE source = 'DRAWN';

-- Q2: the decoded, validated fields of a forwarded provider-signed
-- transaction. The bytes are the intent's request (cleared by Q4 only);
-- tx_hash is their SHA-256 and is kept. outer_signer is the intent's
-- FORWARD_SOURCE signer. Written once.
CREATE TABLE broadcaster.forwarded_txs (
    intent_id      uuid        PRIMARY KEY REFERENCES broadcaster.intents (intent_id),
    tx_hash        bytea       NOT NULL UNIQUE CHECK (octet_length(tx_hash) = 32),
    message_type   text        NOT NULL CHECK (message_type IN ('/svn.leases.v1.MsgAcceptLease',
                                   '/svn.leases.v1.MsgRenewLease', '/svn.leases.v1.MsgEndLease')),
    outer_signer   bytea       NOT NULL REFERENCES broadcaster.signers (address),
    provider_id    text        NOT NULL CHECK (provider_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    account_number bigint      NOT NULL CHECK (account_number >= 0),
    sequence       bigint      NOT NULL CHECK (sequence >= 0),
    timeout_height bigint      NOT NULL CHECK (timeout_height > 0),
    gas            bigint      NOT NULL CHECK (gas BETWEEN 1 AND 20000000),
    recorded_at    timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Q3: content ownership (plan 5.7). PENDING from the register intent's
-- enqueue (or validation, SQL path); EFFECTIVE once that intent is COMMITTED
-- by this signer and the chain showed this signer as submitting host; VOID
-- when it is FAILED. One non-VOID row per content id: the chain refuses a
-- second record of an id in any version (DUPLICATE_ID). EFFECTIVE and VOID
-- rows never change; a VOID row stays as history.
CREATE TABLE broadcaster.content_owners (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    content_id       text        NOT NULL CHECK (content_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    content_version  bigint      NOT NULL CHECK (content_version >= 0),
    caller_principal text        NOT NULL REFERENCES broadcaster.caller_limits (principal),
    principal_id     text        NOT NULL CHECK (principal_id ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    signer_address   bytea       NOT NULL REFERENCES broadcaster.signers (address),
    intent_id        uuid        NOT NULL UNIQUE REFERENCES broadcaster.intents (intent_id),
    status           text        NOT NULL CHECK (status IN ('PENDING', 'EFFECTIVE', 'VOID')),
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    status_at        timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (status_at >= created_at)
);
CREATE UNIQUE INDEX content_owners_one_live ON broadcaster.content_owners (content_id) WHERE status <> 'VOID';

-- Trigger functions.

-- T17 and Q4: a COMMITTED or FAILED intent never changes and is never
-- deleted, with one exception: clearing a FAILED intent none of whose
-- attempts started a send, after every attempt's bytes were cleared. That
-- update sets request, request_json and auth_context to NULL, records
-- auth_context_digest as the SHA-256 of the cleared context's jsonb text and
-- sets cleared_at; every other column stays as it was. A live row keeps its
-- identity columns; what validation sets never changes once validated.
CREATE OR REPLACE FUNCTION broadcaster.intents_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_cleared CONSTANT text[] := ARRAY['request', 'request_json', 'auth_context', 'auth_context_digest', 'cleared_at'];
BEGIN
    IF OLD.state IN ('COMMITTED', 'FAILED') THEN
        IF TG_OP = 'UPDATE' AND OLD.state = 'FAILED' AND OLD.cleared_at IS NULL
           AND NEW.cleared_at IS NOT NULL AND NEW.request IS NULL AND NEW.request_json IS NULL
           AND NEW.auth_context IS NULL
           AND NEW.auth_context_digest IS NOT DISTINCT FROM sha256(convert_to(OLD.auth_context::text, 'UTF8'))
           AND (to_jsonb(NEW) - v_cleared) = (to_jsonb(OLD) - v_cleared)
           AND NOT EXISTS (SELECT 1 FROM broadcaster.attempts a
                            WHERE a.intent_id = OLD.intent_id AND (a.sends_started > 0 OR a.tx_bytes IS NOT NULL)) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'SVN_INTENT_TERMINAL' USING ERRCODE = 'SV001';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.intent_id <> OLD.intent_id OR NEW.caller_principal <> OLD.caller_principal
       OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.kind <> OLD.kind OR NEW.entry <> OLD.entry
       OR NEW.chain_id <> OLD.chain_id OR NEW.created_at <> OLD.created_at OR NEW.expires_at <> OLD.expires_at
       OR NEW.auth_context IS DISTINCT FROM OLD.auth_context
       OR NEW.auth_context_digest IS DISTINCT FROM OLD.auth_context_digest
       OR NEW.cleared_at IS DISTINCT FROM OLD.cleared_at
       OR NEW.sql_digest IS DISTINCT FROM OLD.sql_digest OR NEW.request_json IS DISTINCT FROM OLD.request_json
       OR NEW.attempt_count < OLD.attempt_count
       OR NEW.transitions_recorded < OLD.transitions_recorded
       OR NEW.transitions_suppressed < OLD.transitions_suppressed
       OR (OLD.business_attempt IS NOT NULL AND NEW.business_attempt < OLD.business_attempt) THEN
        RAISE EXCEPTION 'SVN_INTENT_IMMUTABLE_COLUMN' USING ERRCODE = 'SV001';
    END IF;
    IF OLD.validated AND (NOT NEW.validated
       OR NEW.request IS DISTINCT FROM OLD.request OR NEW.request_digest IS DISTINCT FROM OLD.request_digest
       OR NEW.signer_address IS DISTINCT FROM OLD.signer_address
       OR NEW.object_type IS DISTINCT FROM OLD.object_type OR NEW.object_id IS DISTINCT FROM OLD.object_id
       OR NEW.object_version IS DISTINCT FROM OLD.object_version) THEN
        RAISE EXCEPTION 'SVN_INTENT_VALIDATED_COLUMN' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;

-- A terminal attempt never changes (8.5: nothing is rewritten), with one
-- exception (Q4): clearing tx_bytes of an attempt that never started a send
-- while its intent is FAILED, not yet cleared, and none of the intent's
-- attempts started a send; every other column stays as it was. A live
-- attempt keeps its identity columns and bytes, its counters only grow,
-- acked stays true, and its phase only gains knowledge (SIGNED < UNCERTAIN <
-- ACKNOWLEDGED, 9.2).
CREATE OR REPLACE FUNCTION broadcaster.attempts_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    old_rank integer;
    new_rank integer;
BEGIN
    IF OLD.phase NOT IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED') THEN
        IF TG_OP = 'UPDATE' AND OLD.tx_bytes IS NOT NULL AND NEW.tx_bytes IS NULL AND OLD.sends_started = 0
           AND (to_jsonb(NEW) - 'tx_bytes') = (to_jsonb(OLD) - 'tx_bytes')
           AND EXISTS (SELECT 1 FROM broadcaster.intents i
                        WHERE i.intent_id = OLD.intent_id AND i.state = 'FAILED' AND i.cleared_at IS NULL)
           AND NOT EXISTS (SELECT 1 FROM broadcaster.attempts a
                            WHERE a.intent_id = OLD.intent_id AND a.sends_started > 0) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'SVN_ATTEMPT_TERMINAL' USING ERRCODE = 'SV001';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.attempt_id <> OLD.attempt_id OR NEW.intent_id <> OLD.intent_id OR NEW.n <> OLD.n
       OR NEW.business_n <> OLD.business_n OR NEW.signer_address <> OLD.signer_address
       OR NEW.account_number <> OLD.account_number OR NEW.sequence <> OLD.sequence
       OR NEW.timeout_height <> OLD.timeout_height OR NEW.gas <> OLD.gas
       OR NEW.tx_bytes IS DISTINCT FROM OLD.tx_bytes
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

-- A business attempt keeps every column but its outcome, which is set once.
CREATE OR REPLACE FUNCTION broadcaster.business_attempts_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.outcome IS NOT NULL
       OR (to_jsonb(NEW) - 'outcome') <> (to_jsonb(OLD) - 'outcome') THEN
        RAISE EXCEPTION 'SVN_BUSINESS_ATTEMPT_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;

-- A signer keeps its address, class, key name and lock key, and is never
-- deleted.
CREATE FUNCTION broadcaster.signers_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.address <> OLD.address OR NEW.class <> OLD.class
       OR NEW.key_name IS DISTINCT FROM OLD.key_name OR NEW.lock_key IS DISTINCT FROM OLD.lock_key THEN
        RAISE EXCEPTION 'SVN_SIGNER_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER signers_immutable BEFORE UPDATE OR DELETE ON broadcaster.signers
    FOR EACH ROW EXECUTE FUNCTION broadcaster.signers_guard();

-- Q1, Q2: an intent's signer has the class its kind requires. An unknown
-- kind or signer is left to the foreign keys.
CREATE FUNCTION broadcaster.intents_signer_class() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_want text;
    v_have text;
BEGIN
    IF NEW.signer_address IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT k.signer_class INTO v_want FROM broadcaster.kinds k WHERE k.kind = NEW.kind;
    SELECT s.class INTO v_have FROM broadcaster.signers s WHERE s.address = NEW.signer_address;
    IF v_want IS NOT NULL AND v_have IS NOT NULL AND v_have <> v_want THEN
        RAISE EXCEPTION 'SVN_SIGNER_CLASS' USING ERRCODE = 'SV012';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER intents_signer_class BEFORE INSERT OR UPDATE OF signer_address, kind ON broadcaster.intents
    FOR EACH ROW EXECUTE FUNCTION broadcaster.intents_signer_class();

-- An attempt's signer is its intent's signer. A forwarded intent has one
-- attempt, and it is the forwarded transaction itself: its hash, account
-- number, sequence, timeout height and gas are the forwarded_txs row's.
CREATE FUNCTION broadcaster.attempts_insert_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_signer bytea;
    v_class  text;
BEGIN
    SELECT i.signer_address, k.signer_class INTO v_signer, v_class
      FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
     WHERE i.intent_id = NEW.intent_id;
    IF v_signer IS DISTINCT FROM NEW.signer_address THEN
        RAISE EXCEPTION 'SVN_ATTEMPT_SIGNER' USING ERRCODE = 'SV012';
    END IF;
    IF v_class = 'FORWARD_SOURCE' AND (NEW.n <> 1 OR NOT EXISTS (
            SELECT 1 FROM broadcaster.forwarded_txs f
             WHERE f.intent_id = NEW.intent_id AND f.tx_hash = NEW.tx_hash AND f.account_number = NEW.account_number
               AND f.sequence = NEW.sequence AND f.timeout_height = NEW.timeout_height AND f.gas = NEW.gas)) THEN
        RAISE EXCEPTION 'SVN_FORWARD_ATTEMPT' USING ERRCODE = 'SV013';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER attempts_insert_guard BEFORE INSERT ON broadcaster.attempts
    FOR EACH ROW EXECUTE FUNCTION broadcaster.attempts_insert_guard();

-- A drawn business id belongs to a key-signed intent; a forwarded one is
-- the single business attempt of a forwarded intent.
CREATE FUNCTION broadcaster.business_attempts_insert_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_class text;
BEGIN
    SELECT k.signer_class INTO v_class
      FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
     WHERE i.intent_id = NEW.intent_id;
    IF (v_class = 'FORWARD_SOURCE') <> (NEW.source = 'FORWARDED') OR (NEW.source = 'FORWARDED' AND NEW.n <> 1) THEN
        RAISE EXCEPTION 'SVN_FORWARD_BUSINESS_ATTEMPT' USING ERRCODE = 'SV013';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER business_attempts_insert_guard BEFORE INSERT ON broadcaster.business_attempts
    FOR EACH ROW EXECUTE FUNCTION broadcaster.business_attempts_insert_guard();

-- A forwarded_txs row belongs to a validated FORWARD_PROVIDER_TX intent of
-- the same outer signer, the SHA-256 of whose request is tx_hash, and
-- whose business attempt is the forwarded one. It never changes.
CREATE FUNCTION broadcaster.forwarded_txs_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'SVN_FORWARD_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    IF NOT EXISTS (
            SELECT 1 FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
             WHERE i.intent_id = NEW.intent_id AND k.signer_class = 'FORWARD_SOURCE' AND i.validated
               AND i.signer_address = NEW.outer_signer AND i.request IS NOT NULL
               AND sha256(i.request) = NEW.tx_hash
               AND i.object_type = CASE NEW.message_type WHEN '/svn.leases.v1.MsgAcceptLease' THEN 'OFFER' ELSE 'LEASE' END) THEN
        RAISE EXCEPTION 'SVN_FORWARD_INTENT' USING ERRCODE = 'SV013';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER forwarded_txs_write_once BEFORE INSERT OR UPDATE OR DELETE ON broadcaster.forwarded_txs
    FOR EACH ROW EXECUTE FUNCTION broadcaster.forwarded_txs_guard();
CREATE TRIGGER forwarded_txs_no_truncate BEFORE TRUNCATE ON broadcaster.forwarded_txs
    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_statement();

-- A content owner row is created PENDING, for a validated REGISTER_CONTENT
-- intent of the same caller and signer whose object is that content id and
-- version, and whose authorization context names principal_id. Afterwards
-- only status (PENDING to EFFECTIVE or VOID) and status_at change; EFFECTIVE
-- and VOID rows never change; no row is deleted.
CREATE FUNCTION broadcaster.content_owners_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' OR NOT EXISTS (
                SELECT 1 FROM broadcaster.intents i
                 WHERE i.intent_id = NEW.intent_id AND i.kind = 'REGISTER_CONTENT' AND i.validated
                   AND i.state IN ('REQUESTED', 'IN_FLIGHT')
                   AND i.caller_principal = NEW.caller_principal AND i.signer_address = NEW.signer_address
                   AND i.object_type = 'CONTENT' AND i.object_id = NEW.content_id
                   AND i.object_version = NEW.content_version
                   AND i.auth_context ->> 'principal_id' = NEW.principal_id) THEN
            RAISE EXCEPTION 'SVN_CONTENT_OWNER' USING ERRCODE = 'SV014';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' OR OLD.status <> 'PENDING' OR NEW.status NOT IN ('EFFECTIVE', 'VOID')
       OR NEW.status_at < OLD.status_at
       OR (to_jsonb(NEW) - ARRAY['status', 'status_at']) <> (to_jsonb(OLD) - ARRAY['status', 'status_at']) THEN
        RAISE EXCEPTION 'SVN_CONTENT_OWNER_IMMUTABLE' USING ERRCODE = 'SV001';
    END IF;
    RETURN NEW;
END
$fn$;
CREATE TRIGGER content_owners_guard BEFORE INSERT OR UPDATE OR DELETE ON broadcaster.content_owners
    FOR EACH ROW EXECUTE FUNCTION broadcaster.content_owners_guard();
CREATE TRIGGER content_owners_no_truncate BEFORE TRUNCATE ON broadcaster.content_owners
    FOR EACH STATEMENT EXECUTE FUNCTION broadcaster.refuse_statement();

-- At commit (SECURITY DEFINER for the reason given in 0001 at
-- check_committed_evidence):
--   - Q4: a FAILED intent none of whose attempts started a send is cleared;
--   - Q2: a validated FORWARD_PROVIDER_TX intent has its forwarded_txs row;
--   - Q3: a validated REGISTER_CONTENT intent has its content owner row, VOID
--     exactly when the intent is FAILED, EFFECTIVE only when the intent is
--     COMMITTED and its commitment's committing signer is the row's signer.
CREATE FUNCTION broadcaster.check_answers() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $fn$
DECLARE
    v_state     text;
    v_kind      text;
    v_class     text;
    v_validated boolean;
    v_cleared   timestamptz;
    v_status    text;
    v_signer    bytea;
BEGIN
    SELECT i.state, i.kind, k.signer_class, i.validated, i.cleared_at
      INTO v_state, v_kind, v_class, v_validated, v_cleared
      FROM broadcaster.intents i JOIN broadcaster.kinds k ON k.kind = i.kind
     WHERE i.intent_id = NEW.intent_id;
    IF v_state = 'FAILED' AND v_cleared IS NULL AND NOT EXISTS (
            SELECT 1 FROM broadcaster.attempts a WHERE a.intent_id = NEW.intent_id AND a.sends_started > 0) THEN
        RAISE EXCEPTION 'SVN_UNSENT_NOT_CLEARED' USING ERRCODE = 'SV011';
    END IF;
    IF v_validated AND v_class = 'FORWARD_SOURCE' AND NOT EXISTS (
            SELECT 1 FROM broadcaster.forwarded_txs f WHERE f.intent_id = NEW.intent_id) THEN
        RAISE EXCEPTION 'SVN_FORWARD_MISSING' USING ERRCODE = 'SV013';
    END IF;
    IF v_validated AND v_kind = 'REGISTER_CONTENT' THEN
        SELECT o.status, o.signer_address INTO v_status, v_signer
          FROM broadcaster.content_owners o WHERE o.intent_id = NEW.intent_id;
        IF v_status IS NULL OR (v_status = 'VOID') <> (v_state = 'FAILED')
           OR (v_status = 'EFFECTIVE' AND NOT EXISTS (
                SELECT 1 FROM broadcaster.commitments c
                 WHERE c.intent_id = NEW.intent_id AND c.committing_signer = v_signer)) THEN
            RAISE EXCEPTION 'SVN_CONTENT_OWNER_STATE' USING ERRCODE = 'SV014';
        END IF;
    END IF;
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER intents_answers AFTER INSERT OR UPDATE ON broadcaster.intents
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_answers();
CREATE CONSTRAINT TRIGGER content_owners_answers AFTER INSERT OR UPDATE ON broadcaster.content_owners
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_answers();
CREATE CONSTRAINT TRIGGER forwarded_txs_answers AFTER INSERT ON broadcaster.forwarded_txs
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION broadcaster.check_answers();

-- Q4: the clearing step, run in the transaction that made the intent
-- FAILED (and once below for rows FAILED before this migration). When no
-- attempt of a FAILED, not yet cleared intent ever started a send, it clears
-- the attempts' tx_bytes, then the intent's request, request_json and
-- auth_context, keeping their digests, and returns true; otherwise it
-- changes nothing and returns false. A live attempt of a FAILED intent is
-- SV009 (resolve or kill it first).
CREATE FUNCTION broadcaster.clear_unsent(p_intent uuid) RETURNS boolean
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $fn$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM broadcaster.intents i
                    WHERE i.intent_id = p_intent AND i.state = 'FAILED' AND i.cleared_at IS NULL)
       OR EXISTS (SELECT 1 FROM broadcaster.attempts a WHERE a.intent_id = p_intent AND a.sends_started > 0) THEN
        RETURN false;
    END IF;
    IF EXISTS (SELECT 1 FROM broadcaster.attempts a
                WHERE a.intent_id = p_intent AND a.phase IN ('SIGNED', 'UNCERTAIN', 'ACKNOWLEDGED')) THEN
        RAISE EXCEPTION 'SVN_LIVE_ATTEMPT_MISMATCH' USING ERRCODE = 'SV009';
    END IF;
    UPDATE broadcaster.attempts a SET tx_bytes = NULL WHERE a.intent_id = p_intent AND a.tx_bytes IS NOT NULL;
    UPDATE broadcaster.intents i SET request = NULL, request_json = NULL, auth_context = NULL,
           auth_context_digest = sha256(convert_to(i.auth_context::text, 'UTF8')), cleared_at = clock_timestamp()
     WHERE i.intent_id = p_intent;
    RETURN true;
END
$fn$;

-- SQL path (6.3), replacing 0001's function with the same signature, owner,
-- grants and settings. Changes: the kind must be one of broadcaster.kinds
-- (SV004); a replay of a cleared intent compares the authorization context
-- by its kept digest (the context itself is gone).
CREATE OR REPLACE FUNCTION broadcaster.enqueue_intent(
    p_kind text, p_request jsonb, p_auth_context jsonb, p_idempotency_key text, p_expires_in interval)
RETURNS TABLE (intent_id uuid, created boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
SET lock_timeout = '2s'
AS $fn$
#variable_conflict use_column
DECLARE
    v_now         timestamptz := clock_timestamp();
    v_principal   text;
    v_chain       text;
    v_cap         integer;
    v_open        integer;
    v_digest      bytea;
    v_id          uuid;
    v_existing    bytea;
    v_kind        text;
    v_auth        jsonb;
    v_auth_digest bytea;
    v_cleared     timestamptz;
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'SVN_ISOLATION' USING ERRCODE = 'SV007';
    END IF;
    SELECT c.principal INTO v_principal FROM broadcaster.callers c WHERE c.role_name = session_user::text;
    IF v_principal IS NULL THEN
        RAISE EXCEPTION 'SVN_CALLER_UNKNOWN' USING ERRCODE = 'SV003';
    END IF;
    IF p_kind IS NULL OR NOT EXISTS (SELECT 1 FROM broadcaster.kinds k WHERE k.kind = p_kind) THEN
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
    -- (jsonb equality ignores key order; a cleared intent compares the
    -- context's jsonb text digest); anything else is a conflict, as on the
    -- HTTP path, whose digest frames all three (5.4).
    SELECT i.intent_id, i.sql_digest, i.kind, i.auth_context, i.auth_context_digest, i.cleared_at
      INTO v_id, v_existing, v_kind, v_auth, v_auth_digest, v_cleared
      FROM broadcaster.intents i
     WHERE i.caller_principal = v_principal AND i.idempotency_key = p_idempotency_key;
    IF v_id IS NOT NULL THEN
        IF v_existing IS DISTINCT FROM v_digest OR v_kind IS DISTINCT FROM p_kind
           OR (v_cleared IS NULL AND v_auth IS DISTINCT FROM p_auth_context)
           OR (v_cleared IS NOT NULL AND v_auth_digest IS DISTINCT FROM sha256(convert_to(p_auth_context::text, 'UTF8'))) THEN
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

REVOKE ALL ON FUNCTION broadcaster.refuse_statement(), broadcaster.signers_guard(), broadcaster.intents_signer_class(),
    broadcaster.attempts_insert_guard(), broadcaster.business_attempts_insert_guard(),
    broadcaster.forwarded_txs_guard(), broadcaster.content_owners_guard(), broadcaster.check_answers(),
    broadcaster.clear_unsent(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION broadcaster.clear_unsent(uuid) TO svn_broadcaster;

GRANT SELECT ON broadcaster.kinds TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.forwarded_txs TO svn_broadcaster;
GRANT SELECT, INSERT ON broadcaster.content_owners TO svn_broadcaster;
GRANT UPDATE (status, status_at) ON broadcaster.content_owners TO svn_broadcaster;

-- Q4 for rows FAILED before this migration.
SELECT broadcaster.clear_unsent(i.intent_id) FROM broadcaster.intents i WHERE i.state = 'FAILED' AND i.cleared_at IS NULL;
