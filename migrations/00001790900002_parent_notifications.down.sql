-- Stop the worker before rollback. Refuse to discard delivery/audit history.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM parent_notification_outbox) THEN
        RAISE EXCEPTION 'parent notification history exists; preserve it before rollback';
    END IF;
END $$;
DROP TRIGGER absence_parent_notification ON attendances;
DROP FUNCTION capture_absence_notification();
DROP TRIGGER session_parent_notification ON class_sessions;
DROP FUNCTION capture_session_notification();
DROP FUNCTION enqueue_parent_notification(uuid,uuid,text,uuid);
DROP FUNCTION notification_cohort_schedule(uuid);
DROP TABLE parent_notification_outbox;
ALTER TABLE enrollments DROP COLUMN parent_email;
