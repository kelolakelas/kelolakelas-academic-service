ALTER TABLE private_schedule_requests ADD COLUMN chat_version bigint NOT NULL DEFAULT 0;
CREATE TABLE private_request_chat_events (
 id uuid PRIMARY KEY, request_id uuid NOT NULL REFERENCES private_schedule_requests(id), version bigint NOT NULL,
 kind text NOT NULL CHECK(kind IN ('created','approved','rejected','recommendation_accepted')),
 tenant_id uuid NOT NULL, parent_id uuid NOT NULL, student_id uuid NOT NULL, class_id uuid NOT NULL,
 actor_user_id uuid NOT NULL, actor_kind text NOT NULL CHECK(actor_kind IN ('parent','member')),
 transition_user_id uuid NOT NULL, actor_role_id uuid, actor_member_id uuid,
 class_name text NOT NULL, student_first_name text NOT NULL, slots jsonb NOT NULL, recommended_slots jsonb,
 reason text, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(request_id,version),
 CHECK((kind IN ('created','recommendation_accepted') AND actor_kind='parent' AND actor_user_id=parent_id) OR (kind IN ('approved','rejected') AND actor_kind='member' AND actor_role_id IS NOT NULL AND actor_member_id IS NOT NULL))
);
CREATE TABLE private_request_chat_outbox (
 event_id uuid PRIMARY KEY REFERENCES private_request_chat_events(id),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','failed','superseded')),
 attempts integer NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(), lease_token uuid,
 last_error text NOT NULL DEFAULT '', sent_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX private_request_chat_due ON private_request_chat_outbox(available_at,created_at) WHERE status IN ('pending','sending');
