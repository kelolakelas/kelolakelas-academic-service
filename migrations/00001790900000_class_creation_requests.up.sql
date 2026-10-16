CREATE TABLE class_creation_requests (
    tenant_id uuid NOT NULL,
    request_key varchar(128) NOT NULL,
    payload_hash varchar(64) NOT NULL,
    response jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, request_key)
);
