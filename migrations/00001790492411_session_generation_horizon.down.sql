DROP INDEX IF EXISTS uq_class_sessions_schedule_date;
ALTER TABLE class_schedules DROP COLUMN IF EXISTS sessions_generated_until;
