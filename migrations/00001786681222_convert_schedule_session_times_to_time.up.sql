DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'class_schedules' AND column_name = 'start_time' AND data_type <> 'time without time zone') THEN
        ALTER TABLE class_schedules ALTER COLUMN start_time TYPE time without time zone USING start_time::time, ALTER COLUMN end_time TYPE time without time zone USING end_time::time;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'class_sessions' AND column_name = 'start_time' AND data_type <> 'time without time zone') THEN
        ALTER TABLE class_sessions ALTER COLUMN start_time TYPE time without time zone USING start_time::time, ALTER COLUMN end_time TYPE time without time zone USING end_time::time;
    END IF;
END $$;