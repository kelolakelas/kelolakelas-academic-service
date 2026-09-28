CREATE TABLE IF NOT EXISTS private_schedule_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL REFERENCES classes(id),
    student_id uuid NOT NULL REFERENCES students(id),
    parent_id uuid NOT NULL,
    parent_email text NOT NULL,
    billing_cycle varchar(20) NOT NULL CHECK (billing_cycle IN ('monthly', 'quarterly', 'yearly')),
    slots jsonb NOT NULL CHECK (jsonb_typeof(slots) = 'array' AND jsonb_array_length(slots) > 0),
    note text,
    status varchar(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    rejection_reason text,
    created_at timestamp NOT NULL DEFAULT now(),
    decided_at timestamp
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_private_request_pending_student_class ON private_schedule_requests (student_id, class_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_private_request_tenant_status ON private_schedule_requests (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_private_request_parent_created ON private_schedule_requests (parent_id, created_at DESC);
