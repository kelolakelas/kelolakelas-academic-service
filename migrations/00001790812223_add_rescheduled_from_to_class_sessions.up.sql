-- KEL-134: penaut sesi hasil reschedule ke sesi asalnya.
-- Sesi reschedule dibuat dengan ScheduleID = NULL sehingga tidak bisa ditautkan
-- kembali ke jadwal asalnya; tanpa kolom ini pencatatan absensi/daftar hadir
-- sesi reschedule group tidak tahu enrollment mana yang tercakup.
-- Kompatibel: kolom nullable, tanpa backfill (sesi reschedule lama tetap
-- berfungsi lewat fallback status='rescheduled' + ClassID di usecase).
ALTER TABLE class_sessions
    ADD COLUMN IF NOT EXISTS rescheduled_from_session_id uuid;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_class_sessions_rescheduled_from') THEN
        ALTER TABLE class_sessions
            ADD CONSTRAINT fk_class_sessions_rescheduled_from
            FOREIGN KEY (rescheduled_from_session_id) REFERENCES class_sessions(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_class_sessions_rescheduled_from
    ON class_sessions (rescheduled_from_session_id)
    WHERE rescheduled_from_session_id IS NOT NULL;
