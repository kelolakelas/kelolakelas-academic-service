-- KEL-90: sessions are generated continuously up to a rolling horizon by the
-- academic session generation worker.
--
-- sessions_generated_until is the last date whose sessions were already generated
-- (or inherited) for a schedule. The worker only creates sessions after it, so a
-- session a tenant deleted, cancelled or rescheduled is never created again.
ALTER TABLE class_schedules ADD COLUMN IF NOT EXISTS sessions_generated_until date;

-- Backfill. Before KEL-90 a schedule's sessions were always generated through the end
-- of one calendar month (bounded by valid_until, which the worker applies anyway), so
-- the end of the month of the latest session ever linked to the schedule is the range
-- already covered. Soft-deleted, cancelled and rescheduled rows count, because they
-- are exactly the sessions that must not come back. Schedules without any session
-- stay NULL and are filled from the start of the current month (bounded by
-- valid_from). Only NULL rows are touched, so a repeated run is a no-op.
UPDATE class_schedules cs
SET sessions_generated_until = (date_trunc('month', latest.last_date) + interval '1 month - 1 day')::date
FROM (
    SELECT schedule_id, MAX(session_date) AS last_date
    FROM class_sessions
    WHERE schedule_id IS NOT NULL
    GROUP BY schedule_id
) latest
WHERE latest.schedule_id = cs.id
  AND cs.sessions_generated_until IS NULL;

-- One session per schedule and date, soft-deleted and cancelled rows included. The
-- worker inserts with ON CONFLICT DO NOTHING against this index, so a repeated or
-- concurrent run can neither duplicate a session nor recreate one the tenant removed.
-- One-off reschedules (schedule_id IS NULL) are not constrained. No code path has
-- produced duplicates before this migration; if a database holds some anyway the
-- migration stops here instead of deleting tenant data, and they must be resolved by
-- hand first.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM class_sessions
        WHERE schedule_id IS NOT NULL
        GROUP BY schedule_id, session_date
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'class_sessions holds more than one row for the same schedule_id and session_date; resolve the duplicates before applying uq_class_sessions_schedule_date';
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS uq_class_sessions_schedule_date
    ON class_sessions (schedule_id, session_date)
    WHERE schedule_id IS NOT NULL;
