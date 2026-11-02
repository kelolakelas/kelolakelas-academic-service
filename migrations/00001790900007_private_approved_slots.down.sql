DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM private_schedule_requests WHERE approved_slots IS NOT NULL) THEN
    RAISE EXCEPTION 'Cannot remove persisted approval selections';
  END IF;
END $$;
ALTER TABLE private_schedule_requests DROP CONSTRAINT private_approved_slots_nonempty;
ALTER TABLE private_schedule_requests DROP COLUMN approved_slots;
