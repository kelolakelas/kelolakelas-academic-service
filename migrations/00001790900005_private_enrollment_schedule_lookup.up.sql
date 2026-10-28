CREATE INDEX IF NOT EXISTS idx_class_schedules_live_enrollment
    ON class_schedules (enrollment_id)
    WHERE deleted_at IS NULL AND enrollment_id IS NOT NULL;
