CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollments_idempotency_key
    ON enrollments (idempotency_key)
    WHERE idempotency_key IS NOT NULL;