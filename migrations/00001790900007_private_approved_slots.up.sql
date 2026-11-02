ALTER TABLE private_schedule_requests ADD COLUMN approved_slots JSONB;
ALTER TABLE private_schedule_requests ADD CONSTRAINT private_approved_slots_nonempty
CHECK (approved_slots IS NULL OR (jsonb_typeof(approved_slots) = 'array' AND jsonb_array_length(approved_slots) > 0));
