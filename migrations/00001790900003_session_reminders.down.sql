DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session_reminder_enrollments) THEN
        RAISE EXCEPTION 'session reminder coverage exists; preserve it before rollback';
    END IF;
END $$;
DROP INDEX session_reminder_candidates;
DROP FUNCTION session_reminder_covered(uuid,uuid);
DROP TABLE session_reminder_enrollments;
