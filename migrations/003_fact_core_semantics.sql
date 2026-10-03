-- Core delivery and verification identity is explicit and queryable. Existing
-- facts remain NULL; never infer these values from body or data.
ALTER TABLE task_facts ADD COLUMN baseline TEXT CHECK (baseline IS NULL OR length(trim(baseline)) > 0);
ALTER TABLE task_facts ADD COLUMN fingerprint TEXT CHECK (fingerprint IS NULL OR length(trim(fingerprint)) > 0);
ALTER TABLE task_facts ADD COLUMN accepted_commit TEXT CHECK (accepted_commit IS NULL OR length(trim(accepted_commit)) > 0);
ALTER TABLE task_facts ADD COLUMN verdict TEXT CHECK (verdict IS NULL OR verdict IN ('PASS', 'RC', 'BLOCKED'));
