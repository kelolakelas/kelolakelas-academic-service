ALTER TABLE enrollments DROP CONSTRAINT IF EXISTS fk_enrollments_schedule;
DROP INDEX IF EXISTS idx_class_schedules_class_day;
DROP INDEX IF EXISTS idx_enrollments_schedule_status;
ALTER TABLE enrollments DROP COLUMN IF EXISTS schedule_id;
ALTER TABLE class_schedules DROP CONSTRAINT IF EXISTS class_schedules_capacity_non_negative;
ALTER TABLE class_schedules DROP COLUMN IF EXISTS capacity;
