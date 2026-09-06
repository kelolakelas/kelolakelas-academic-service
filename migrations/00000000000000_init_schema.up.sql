CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, name varchar(255) NOT NULL, description jsonb,
    created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(), deleted_at timestamp
);
CREATE INDEX IF NOT EXISTS idx_categories_tenant_id ON categories (tenant_id);
CREATE INDEX IF NOT EXISTS idx_categories_deleted_at ON categories (deleted_at);
CREATE TABLE IF NOT EXISTS classes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, category_id uuid NOT NULL,
    name varchar(255) NOT NULL, description jsonb, type varchar(50) NOT NULL, price bigint NOT NULL,
    capacity integer, is_published boolean NOT NULL DEFAULT false, enrollment_status varchar(20) NOT NULL DEFAULT 'open',
    created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(), deleted_at timestamp,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
CREATE INDEX IF NOT EXISTS idx_classes_tenant_category ON classes (tenant_id, category_id);
CREATE INDEX IF NOT EXISTS idx_classes_deleted_at ON classes (deleted_at);
CREATE TABLE IF NOT EXISTS students (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), parent_id uuid NOT NULL, first_name varchar(255) NOT NULL,
    last_name varchar(255), nickname varchar(100), gender varchar(10), date_of_birth date,
    created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(), deleted_at timestamp
);
CREATE INDEX IF NOT EXISTS idx_students_parent_id ON students (parent_id);
CREATE INDEX IF NOT EXISTS idx_students_deleted_at ON students (deleted_at);
CREATE TABLE IF NOT EXISTS enrollments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, student_id uuid NOT NULL, class_id uuid NOT NULL,
    schedule_id uuid, status varchar(50) NOT NULL, billing_cycle varchar(20) NOT NULL DEFAULT 'monthly',
    idempotency_key varchar(255), payment_transaction_id uuid, checkout_session_url text, payment_status varchar(30) DEFAULT 'pending',
    gross_amount bigint NOT NULL DEFAULT 0, joined_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(), deleted_at timestamp,
    FOREIGN KEY (student_id) REFERENCES students(id), FOREIGN KEY (class_id) REFERENCES classes(id)
);
CREATE INDEX IF NOT EXISTS idx_enrollments_tenant_status ON enrollments (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_enrollments_student_class ON enrollments (student_id, class_id);
CREATE INDEX IF NOT EXISTS idx_enrollments_deleted_at ON enrollments (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollments_idempotency_key ON enrollments (idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_student_class_active ON enrollments (student_id, class_id) WHERE status IN ('pending', 'active') AND deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS class_schedules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), class_id uuid NOT NULL, enrollment_id uuid, tutor_id uuid,
    capacity integer NOT NULL DEFAULT 1, location varchar(255), day_of_week integer NOT NULL, start_time time NOT NULL, end_time time NOT NULL,
    valid_from date, valid_until date, deleted_at timestamp, CONSTRAINT class_schedules_capacity_non_negative CHECK (capacity > 0),
    FOREIGN KEY (class_id) REFERENCES classes(id)
);
CREATE INDEX IF NOT EXISTS idx_class_schedules_class_day ON class_schedules (class_id, day_of_week) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_class_schedules_deleted_at ON class_schedules (deleted_at);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_enrollments_schedule') THEN
        ALTER TABLE enrollments ADD CONSTRAINT fk_enrollments_schedule FOREIGN KEY (schedule_id) REFERENCES class_schedules(id);
    END IF;
END $$;
CREATE TABLE IF NOT EXISTS class_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), class_id uuid NOT NULL, schedule_id uuid, enrollment_id uuid, tutor_id uuid NOT NULL,
    session_date date NOT NULL, start_time time NOT NULL, end_time time NOT NULL, status varchar(50) NOT NULL DEFAULT 'scheduled', deleted_at timestamp,
    FOREIGN KEY (class_id) REFERENCES classes(id)
);
CREATE INDEX IF NOT EXISTS idx_class_sessions_class_date ON class_sessions (class_id, session_date);
CREATE INDEX IF NOT EXISTS idx_class_sessions_deleted_at ON class_sessions (deleted_at);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_class_sessions_schedule') THEN
        ALTER TABLE class_sessions ADD CONSTRAINT fk_class_sessions_schedule FOREIGN KEY (schedule_id) REFERENCES class_schedules(id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_class_sessions_enrollment') THEN
        ALTER TABLE class_sessions ADD CONSTRAINT fk_class_sessions_enrollment FOREIGN KEY (enrollment_id) REFERENCES enrollments(id);
    END IF;
END $$;
CREATE TABLE IF NOT EXISTS class_teachers (
    class_id uuid NOT NULL, teacher_id uuid NOT NULL, assigned_at timestamp NOT NULL DEFAULT now(), PRIMARY KEY (class_id, teacher_id),
    FOREIGN KEY (class_id) REFERENCES classes(id)
);
CREATE TABLE IF NOT EXISTS attendances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), enrollment_id uuid NOT NULL, session_id uuid NOT NULL, date date NOT NULL,
    status varchar(50) NOT NULL, created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(),
    CONSTRAINT uq_attendances_session_enrollment UNIQUE (session_id, enrollment_id), FOREIGN KEY (enrollment_id) REFERENCES enrollments(id), FOREIGN KEY (session_id) REFERENCES class_sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_attendances_enrollment_date ON attendances (enrollment_id, date);
CREATE TABLE IF NOT EXISTS student_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid, student_id uuid NOT NULL, author_id uuid NOT NULL,
    note_type varchar(50) NOT NULL, content text NOT NULL, created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(),
    FOREIGN KEY (student_id) REFERENCES students(id)
);
CREATE INDEX IF NOT EXISTS idx_student_notes_tenant_student ON student_notes (tenant_id, student_id);
CREATE TABLE IF NOT EXISTS reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, enrollment_id uuid NOT NULL, reporter_id uuid NOT NULL,
    title varchar(255) NOT NULL, evaluation_notes text, score numeric(5,2), created_at timestamp NOT NULL DEFAULT now(), updated_at timestamp NOT NULL DEFAULT now(), deleted_at timestamp,
    FOREIGN KEY (enrollment_id) REFERENCES enrollments(id)
);
CREATE INDEX IF NOT EXISTS idx_reports_tenant_id ON reports (tenant_id);
CREATE INDEX IF NOT EXISTS idx_reports_enrollment_id ON reports (enrollment_id);
CREATE INDEX IF NOT EXISTS idx_reports_reporter_id ON reports (reporter_id);
CREATE INDEX IF NOT EXISTS idx_reports_deleted_at ON reports (deleted_at);
CREATE TABLE IF NOT EXISTS tenant_location_snapshots (
    tenant_id uuid PRIMARY KEY, name varchar(255) NOT NULL, address_formatted varchar(500), latitude decimal(10,7), longitude decimal(10,7),
    is_active boolean NOT NULL DEFAULT false, updated_at timestamp NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_classes_catalog_filters ON classes (is_published, enrollment_status, type, price, created_at) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS seed_versions (filename varchar(255) PRIMARY KEY, checksum varchar(64) NOT NULL, applied_at timestamp NOT NULL DEFAULT now());
