-- Retain committed retry keys/responses on application rollback.
-- Dropping this table would allow a previously committed request to be duplicated.
SELECT 1;
