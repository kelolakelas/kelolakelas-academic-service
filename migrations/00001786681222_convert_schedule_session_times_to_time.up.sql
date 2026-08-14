ALTER TABLE class_schedules
    ALTER COLUMN start_time TYPE time without time zone
    USING start_time::time,
    ALTER COLUMN end_time TYPE time without time zone
    USING end_time::time;

ALTER TABLE class_sessions
    ALTER COLUMN start_time TYPE time without time zone
    USING start_time::time,
    ALTER COLUMN end_time TYPE time without time zone
    USING end_time::time;