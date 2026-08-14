ALTER TABLE class_schedules
    ALTER COLUMN start_time TYPE timestamp without time zone
    USING (DATE '1970-01-01' + start_time),
    ALTER COLUMN end_time TYPE timestamp without time zone
    USING (DATE '1970-01-01' + end_time);

ALTER TABLE class_sessions
    ALTER COLUMN start_time TYPE timestamp without time zone
    USING (DATE '1970-01-01' + start_time),
    ALTER COLUMN end_time TYPE timestamp without time zone
    USING (DATE '1970-01-01' + end_time);