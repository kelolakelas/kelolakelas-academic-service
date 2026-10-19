-- KEL-155. Capture recipients and content in the same transaction as the mutation.
ALTER TABLE enrollments ADD COLUMN IF NOT EXISTS parent_email text;

CREATE TABLE parent_notification_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    parent_id uuid NOT NULL,
    channel text NOT NULL CHECK (channel IN ('chat','email')),
    recipient_email text NOT NULL DEFAULT '',
    sender_email text NOT NULL DEFAULT '',
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    first_attempt_at timestamptz,
    sent_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id,parent_id,channel)
);
CREATE INDEX parent_notification_due ON parent_notification_outbox (available_at,created_at)
    WHERE status IN ('pending','sending');

-- Resolve replacements recursively, never by guessing from another class/tenant.
CREATE FUNCTION notification_cohort_schedule(session_id uuid) RETURNS uuid
LANGUAGE sql STABLE AS $$
    WITH RECURSIVE chain AS (
        SELECT id,class_id,schedule_id,rescheduled_from_session_id,ARRAY[id] AS visited
        FROM class_sessions WHERE id=session_id
        UNION ALL
        SELECT s.id,s.class_id,s.schedule_id,s.rescheduled_from_session_id,c.visited || s.id
        FROM class_sessions s JOIN chain c ON s.id=c.rescheduled_from_session_id AND s.class_id=c.class_id
        WHERE NOT s.id=ANY(c.visited)
    ) SELECT schedule_id FROM chain WHERE schedule_id IS NOT NULL LIMIT 1;
$$;

CREATE FUNCTION enqueue_parent_notification(sid uuid, only_enrollment uuid, message text, eid uuid)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO parent_notification_outbox(event_id,tenant_id,parent_id,channel,recipient_email,body)
    SELECT eid, recipients.tenant_id, recipients.parent_id, recipients.channel,
           CASE WHEN recipients.channel='email' THEN recipients.email ELSE '' END,
           left(message || E'\nSiswa: ' || left(recipients.names,1000),2000)
    FROM (
        SELECT c.tenant_id,st.parent_id,channels.channel,string_agg(DISTINCT st.first_name, ', ' ORDER BY st.first_name) AS names,
               coalesce((array_agg(e.parent_email ORDER BY e.joined_at DESC,e.id)
                         FILTER (WHERE nullif(btrim(e.parent_email),'') IS NOT NULL))[1],'') AS email
        FROM class_sessions s
        JOIN classes c ON c.id=s.class_id AND c.deleted_at IS NULL
        JOIN enrollments e ON e.class_id=c.id AND e.tenant_id=c.tenant_id
            AND e.status='active' AND e.deleted_at IS NULL
        JOIN students st ON st.id=e.student_id AND st.deleted_at IS NULL
        CROSS JOIN (VALUES ('chat'),('email')) channels(channel)
        WHERE s.id=sid AND s.deleted_at IS NULL AND s.status NOT IN ('cancelled','rescheduled')
          AND (channels.channel='chat' OR nullif(btrim(e.parent_email),'') IS NOT NULL)
          AND (only_enrollment IS NULL OR e.id=only_enrollment)
          AND (CASE WHEN s.enrollment_id IS NOT NULL THEN e.id=s.enrollment_id
                    ELSE e.schedule_id=notification_cohort_schedule(s.id) END)
        GROUP BY c.tenant_id,st.parent_id,channels.channel
    ) recipients
    ON CONFLICT (event_id,parent_id,channel) DO NOTHING;
END;
$$;

CREATE FUNCTION capture_session_notification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE origin class_sessions; cname text; msg text;
BEGIN
    IF NEW.deleted_at IS NOT NULL OR NEW.status<>'scheduled' THEN RETURN NEW; END IF;
    SELECT name INTO cname FROM classes WHERE id=NEW.class_id AND deleted_at IS NULL;
    IF TG_OP='INSERT' THEN
        IF NEW.rescheduled_from_session_id IS NULL THEN RETURN NEW; END IF;
        SELECT * INTO origin FROM class_sessions
            WHERE id=NEW.rescheduled_from_session_id AND class_id=NEW.class_id;
        IF NOT FOUND THEN RETURN NEW; END IF;
        msg := 'Jadwal kelas ' || cname || ' berubah dari ' || to_char(origin.session_date,'DD-MM-YYYY') ||
            ' ' || origin.start_time::text || '–' || origin.end_time::text || ' WIB menjadi ' ||
            to_char(NEW.session_date,'DD-MM-YYYY') || ' ' || NEW.start_time::text || '–' || NEW.end_time::text || ' WIB.';
    ELSE
        IF NEW.tutor_id IS NOT DISTINCT FROM OLD.tutor_id THEN RETURN NEW; END IF;
        msg := 'Pengajar kelas ' || cname || ' diganti untuk sesi ' || to_char(NEW.session_date,'DD-MM-YYYY') ||
            ' ' || NEW.start_time::text || '–' || NEW.end_time::text || ' WIB. Waktu sesi tetap.';
    END IF;
    PERFORM enqueue_parent_notification(NEW.id,NULL,msg,gen_random_uuid());
    RETURN NEW;
END;
$$;
CREATE TRIGGER session_parent_notification AFTER INSERT OR UPDATE ON class_sessions
    FOR EACH ROW EXECUTE FUNCTION capture_session_notification();

CREATE FUNCTION capture_absence_notification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s class_sessions; cname text;
BEGIN
    IF NEW.status<>'absent' THEN RETURN NEW; END IF;
    IF TG_OP='UPDATE' THEN
        IF OLD.status='absent' THEN RETURN NEW; END IF;
    END IF;
    SELECT * INTO s FROM class_sessions WHERE id=NEW.session_id;
    SELECT name INTO cname FROM classes WHERE id=s.class_id AND deleted_at IS NULL;
    PERFORM enqueue_parent_notification(NEW.session_id,NEW.enrollment_id,
        'Ketidakhadiran (alfa) pada kelas ' || cname || ', sesi ' || to_char(s.session_date,'DD-MM-YYYY') ||
        ' ' || s.start_time::text || '–' || s.end_time::text || ' WIB.',gen_random_uuid());
    RETURN NEW;
END;
$$;
CREATE TRIGGER absence_parent_notification AFTER INSERT OR UPDATE ON attendances
    FOR EACH ROW EXECUTE FUNCTION capture_absence_notification();
