-- Unique follow-up version: 1790900000 already belongs to private request payment methods.
-- Preserve retry history if an earlier deployment created this table directly.
CREATE TABLE IF NOT EXISTS class_creation_requests (
    tenant_id uuid NOT NULL,
    request_key varchar(128) NOT NULL,
    payload_hash varchar(64) NOT NULL,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, request_key)
);
