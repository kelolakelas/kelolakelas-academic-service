ALTER TABLE classes ADD COLUMN private_slot_limit integer;
ALTER TABLE classes ADD CONSTRAINT classes_private_slot_limit_check
    CHECK (private_slot_limit IS NULL OR (type = 'private' AND private_slot_limit > 0));
