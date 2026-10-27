DROP TABLE private_request_chat_outbox;
DROP TABLE private_request_chat_events;
ALTER TABLE private_schedule_requests DROP COLUMN chat_version;
