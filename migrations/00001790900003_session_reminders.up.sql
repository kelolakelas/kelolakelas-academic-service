-- KEL-156. Keep coverage durable across restarts, window changes and replicas.
CREATE INDEX session_reminder_candidates ON class_sessions(session_date,id)
 WHERE status='scheduled' AND deleted_at IS NULL;
CREATE TABLE session_reminder_enrollments (
    session_id uuid NOT NULL REFERENCES class_sessions(id),
    enrollment_id uuid NOT NULL REFERENCES enrollments(id),
    event_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id,enrollment_id)
);
CREATE INDEX session_reminder_event ON session_reminder_enrollments(event_id);

-- A reschedule after a reminder is covered by the existing change notification.
CREATE FUNCTION session_reminder_covered(sid uuid,eid uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
 WITH RECURSIVE chain AS (
  SELECT id,class_id,rescheduled_from_session_id,ARRAY[id] AS visited FROM class_sessions WHERE id=sid
  UNION ALL
  SELECT s.id,s.class_id,s.rescheduled_from_session_id,c.visited || s.id
  FROM class_sessions s JOIN chain c ON s.id=c.rescheduled_from_session_id AND s.class_id=c.class_id
  WHERE NOT s.id=ANY(c.visited)
 ) SELECT EXISTS (SELECT 1 FROM chain JOIN session_reminder_enrollments m ON m.session_id=chain.id WHERE m.enrollment_id=eid);
$$;
