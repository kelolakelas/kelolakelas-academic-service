ALTER TABLE private_schedule_requests DROP CONSTRAINT private_schedule_requests_status_check;
UPDATE private_schedule_requests SET status = 'rejected' WHERE status = 'declined';
ALTER TABLE private_schedule_requests ADD CONSTRAINT private_schedule_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'));
ALTER TABLE private_schedule_requests DROP COLUMN recommended_slots;
