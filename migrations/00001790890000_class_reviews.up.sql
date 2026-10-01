CREATE TABLE IF NOT EXISTS class_reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    enrollment_id uuid NOT NULL REFERENCES enrollments(id),
    class_id uuid NOT NULL REFERENCES classes(id),
    rating smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment text CHECK (char_length(comment) <= 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_reviews_enrollment_unique UNIQUE (enrollment_id)
);
CREATE INDEX IF NOT EXISTS class_reviews_class_created_idx ON class_reviews (class_id, created_at DESC, id DESC);
