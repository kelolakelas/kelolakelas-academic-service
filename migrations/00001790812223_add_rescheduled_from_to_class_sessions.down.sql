-- KEL-134 rollback: hapus penaut reschedule. Data absensi tidak tersentuh.
ALTER TABLE class_sessions DROP CONSTRAINT IF EXISTS fk_class_sessions_rescheduled_from;
DROP INDEX IF EXISTS idx_class_sessions_rescheduled_from;
ALTER TABLE class_sessions DROP COLUMN IF EXISTS rescheduled_from_session_id;
