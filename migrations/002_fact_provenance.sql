-- Optional, self-reported fact provenance. Existing facts remain NULL; do not
-- derive historical values from actor labels, bodies, or other fact data.
ALTER TABLE task_facts ADD COLUMN provenance_role TEXT;
ALTER TABLE task_facts ADD COLUMN provenance_session TEXT;
ALTER TABLE task_facts ADD COLUMN provenance_harness TEXT;
ALTER TABLE task_facts ADD COLUMN provenance_model TEXT;
