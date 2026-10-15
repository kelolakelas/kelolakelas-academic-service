-- Apply before deploying academic code; old writers continue using the default.
ALTER TABLE private_schedule_requests
    ADD COLUMN payment_method VARCHAR(2) NOT NULL DEFAULT 'VC';
ALTER TABLE private_schedule_requests
    ADD CONSTRAINT private_request_payment_method_check
    CHECK (payment_method IN ('VC', 'VA', 'BC', 'SP', 'NQ'));
