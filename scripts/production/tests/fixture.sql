-- Isolated CI only: actual application schema, not reduced mock tables.
INSERT INTO sys_user (id, username, password_hash, is_super_admin, session_version, created_at, updated_at)
VALUES (101, 'recovery-drill-only', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', true, 7, now(), now());
INSERT INTO sys_role (id, code, name, created_at, updated_at) VALUES (102, 'recovery-reviewer', 'Recovery reviewer', now(), now());
INSERT INTO sys_user_role (id, user_id, role_id, created_at, updated_at) VALUES (103, 101, 102, now(), now());
INSERT INTO glean_paper (id, identity_key, title, authors, evidence, created_at, updated_at)
VALUES (201, 'recovery-drill-paper', 'Original immutable title', '["Original author"]', '[{"source":"original-private-archive","value":"raw-evidence-sentinel"}]', now(), now());
INSERT INTO glean_membership (id, paper_id, topic, original_id, categories, dimensions, raw, created_at, updated_at)
VALUES (202, 201, 'recovery-topic', 'private-original-001', '[]', '{}', '{"original_archive":"private-original-sentinel","title":"Original immutable title"}', now(), now());
INSERT INTO glean_topic (id, slug, name, term_groups, created_at, updated_at)
VALUES (203, 'recovery-topic', 'Recovery topic', '[["recovery"]]', now(), now());
INSERT INTO glean_paper_review (id, paper_id, topic, status, note, labels, version, created_by, created_at, updated_at)
VALUES (204, 201, 'recovery-topic', 'included', 'human-review-sentinel', '["review-label"]', 3, 101, now(), now());
INSERT INTO glean_paper_correction (id, paper_id, fields, reason, version, created_by, created_at, updated_at)
VALUES (205, 201, '{"title":"Corrected display title"}', 'correction-sentinel', 4, 101, now(), now());
INSERT INTO glean_catalog_change (id, paper_id, topic, kind, "before", "after", created_by, created_at, updated_at)
VALUES (206, 201, 'recovery-topic', 'correction', '{"title":"Original immutable title"}', '{"title":"Corrected display title"}', 101, now(), now());
INSERT INTO glean_paper_state (id, paper_id, user_id, hidden, read_status, created_at, updated_at)
VALUES (207, 201, 101, true, 'read', now(), now());
INSERT INTO glean_observation (id, paper_id, run_id, source, source_id, version, content_hash, observed_at, metadata, publication_events, review, created_at, updated_at)
VALUES (208, 201, 301, 'crossref', 'recovery-source-id', 'v1', 'recovery-content-hash', now(), '{"title":"observed-title-sentinel"}', '[]', '{"is_mock":true}', now(), now());
INSERT INTO glean_monitor_state (id, key, snapshot, revision, created_at, updated_at)
VALUES (301, 'recovery-drill', '{"cursor":"cursor-sentinel","watermark":"2026-10-27T00:00:00Z","lease":{"owner":"previous-process"},"pending":["recovery-source-id"]}', 9, now(), now());
-- pg_dump must also preserve migration bookkeeping outside public.
CREATE SCHEMA atlas_schema_revisions;
CREATE TABLE atlas_schema_revisions.recovery_fixture (version text NOT NULL);
INSERT INTO atlas_schema_revisions.recovery_fixture VALUES ('migration-revision-sentinel');
