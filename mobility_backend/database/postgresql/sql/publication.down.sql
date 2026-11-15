DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['agencies','routes','stops','services','calendars','service_exceptions','shapes','shape_points','journey_patterns','journey_pattern_stops','trips','stop_times','frequencies'] LOOP
  EXECUTE format('DROP VIEW gtfs.active_%I',t);
  EXECUTE format('DROP TRIGGER snapshot_write_guard ON gtfs.%I',t);
 END LOOP;
END $$;
DROP TRIGGER publication_ready_guard ON ingest.feed_publications;
DROP TRIGGER validation_write_guard ON ingest.validation_results;
DROP TRIGGER feed_transition_guard ON ingest.feed_versions;
DROP FUNCTION ingest.promote_feed(bigint,bigint,bigint,text,text), ingest.guard_publication(),
 ingest.guard_validation_write(), ingest.guard_feed_transition(), ingest.guard_snapshot_write();
REVOKE USAGE ON SCHEMA public,gtfs,ingest FROM mobility_app,mobility_importer;
-- NOLOGIN group roles may serve another database in this new cluster; do not drop them.
