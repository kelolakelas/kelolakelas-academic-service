ALTER TABLE class_schedules
    ADD COLUMN IF NOT EXISTS capacity integer;

UPDATE class_schedules cs
SET capacity = COALESCE(c.capacity, CASE WHEN c.type = 'private' THEN 1 ELSE 1 END)
FROM classes c
WHERE c.id = cs.class_id;

ALTER TABLE class_schedules
    ALTER COLUMN capacity SET DEFAULT 1,
    ALTER COLUMN capacity SET NOT NULL;

ALTER TABLE enrollments
    ADD COLUMN IF NOT EXISTS schedule_id uuid;

CREATE INDEX IF NOT EXISTS idx_enrollments_schedule_status
    ON enrollments (schedule_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_class_schedules_class_day
    ON class_schedules (class_id, day_of_week)
    WHERE deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'class_schedules_capacity_non_negative') THEN
        ALTER TABLE class_schedules ADD CONSTRAINT class_schedules_capacity_non_negative CHECK (capacity > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_enrollments_schedule') THEN
        ALTER TABLE enrollments ADD CONSTRAINT fk_enrollments_schedule FOREIGN KEY (schedule_id) REFERENCES class_schedules(id);
    END IF;
END $$;

-- classes.capacity is retained for backwards-compatible reads and legacy data recovery.
-- New writes must use class_schedules.capacity.
