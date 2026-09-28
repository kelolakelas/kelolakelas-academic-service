ALTER TABLE private_schedule_requests
    ADD COLUMN recommended_slots jsonb CHECK (recommended_slots IS NULL OR (jsonb_typeof(recommended_slots) = 'array' AND jsonb_array_length(recommended_slots) > 0));
ALTER TABLE private_schedule_requests DROP CONSTRAINT private_schedule_requests_status_check;
ALTER TABLE private_schedule_requests ADD CONSTRAINT private_schedule_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'declined'));
