-- Serialize snapshot writes against validation/promotion. Row locks are held until commit.
CREATE FUNCTION ingest.guard_snapshot_write() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE v_state text;
BEGIN
 IF TG_OP <> 'INSERT' THEN
  SELECT state INTO v_state FROM ingest.feed_versions WHERE id = OLD.feed_version_id FOR SHARE;
  IF v_state IS DISTINCT FROM 'staging' THEN RAISE EXCEPTION 'Snapshot is immutable outside staging' USING ERRCODE = '55000'; END IF;
 END IF;
 IF TG_OP <> 'DELETE' THEN
  SELECT state INTO v_state FROM ingest.feed_versions WHERE id = NEW.feed_version_id FOR SHARE;
  IF v_state IS DISTINCT FROM 'staging' THEN RAISE EXCEPTION 'Snapshot is immutable outside staging' USING ERRCODE = '55000'; END IF;
  RETURN NEW;
 END IF;
 RETURN OLD;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['agencies','routes','stops','services','calendars','service_exceptions','shapes','shape_points','journey_patterns','journey_pattern_stops','trips','stop_times','frequencies'] LOOP
  EXECUTE format('CREATE TRIGGER snapshot_write_guard BEFORE INSERT OR UPDATE OR DELETE ON gtfs.%I FOR EACH ROW EXECUTE FUNCTION ingest.guard_snapshot_write()',t);
 END LOOP;
END $$;

CREATE FUNCTION ingest.guard_feed_transition() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
 IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Feed deletion is not a runtime operation' USING ERRCODE = '55000'; END IF;
 IF TG_OP = 'INSERT' THEN
  IF NEW.state <> 'staging' THEN RAISE EXCEPTION 'New feeds must start in staging' USING ERRCODE = '55000'; END IF;
  RETURN NEW;
 END IF;
 IF OLD.state IN ('ready','failed') THEN RAISE EXCEPTION 'Terminal feed versions are immutable' USING ERRCODE = '55000'; END IF;
 IF NEW.feed_source_id <> OLD.feed_source_id OR NEW.checksum_sha256 <> OLD.checksum_sha256
    OR NEW.normalizer_version <> OLD.normalizer_version OR NEW.profile_version <> OLD.profile_version THEN
  RAISE EXCEPTION 'Feed identity is immutable' USING ERRCODE = '55000';
 END IF;
 IF NEW.state <> OLD.state AND NOT ((OLD.state = 'staging' AND NEW.state IN ('validating','failed'))
    OR (OLD.state = 'validating' AND NEW.state IN ('ready','failed'))) THEN
  RAISE EXCEPTION 'Invalid feed state transition' USING ERRCODE = '55000';
 END IF;
 IF NEW.state = 'ready' AND OLD.state <> 'ready' THEN
  -- Full parser/profile validation is a future importer responsibility, explicitly attested here.
  IF NOT EXISTS (SELECT 1 FROM ingest.validation_results WHERE feed_version_id = NEW.id AND rule_code = 'profile.complete' AND passed AND severity = 'info')
    OR EXISTS (SELECT 1 FROM ingest.validation_results WHERE feed_version_id = NEW.id AND severity = 'error' AND NOT passed)
    OR NOT EXISTS (SELECT 1 FROM ingest.import_runs WHERE feed_version_id = NEW.id AND state = 'succeeded') THEN
   RAISE EXCEPTION 'Successful import and profile validation required' USING ERRCODE = '23514';
  END IF;
  IF EXISTS (SELECT 1 FROM gtfs.services s LEFT JOIN gtfs.calendars c ON (c.feed_version_id,c.service_id) = (s.feed_version_id,s.id)
       WHERE s.feed_version_id = NEW.id AND (s.has_weekly_calendar <> (c.service_id IS NOT NULL)
       OR (NOT s.has_weekly_calendar AND NOT EXISTS (SELECT 1 FROM gtfs.service_exceptions e WHERE (e.feed_version_id,e.service_id) = (s.feed_version_id,s.id))))) THEN
   RAISE EXCEPTION 'Service calendar or exception coverage is inconsistent' USING ERRCODE = '23514';
  END IF;
  IF EXISTS (SELECT 1 FROM (SELECT start_service_seconds,
       max(end_service_seconds) OVER (PARTITION BY trip_id ORDER BY start_service_seconds ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) AS prior_end
       FROM gtfs.frequencies WHERE feed_version_id=NEW.id) f WHERE start_service_seconds<prior_end) THEN
   RAISE EXCEPTION 'Frequency intervals overlap' USING ERRCODE = '23514';
  END IF;
  IF EXISTS (SELECT 1 FROM (SELECT coalesce(arrival_service_seconds,departure_service_seconds) AS current_seconds,
       max(coalesce(departure_service_seconds,arrival_service_seconds)) OVER (PARTITION BY trip_id ORDER BY stop_sequence ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) AS prior_time
       FROM gtfs.stop_times WHERE feed_version_id=NEW.id) s WHERE s.current_seconds<s.prior_time) THEN
   RAISE EXCEPTION 'Service time decreases along trip' USING ERRCODE = '23514';
  END IF;
  IF EXISTS (SELECT 1 FROM gtfs.trips t JOIN gtfs.journey_patterns p ON (p.feed_version_id,p.id)=(t.feed_version_id,t.pattern_id)
       WHERE t.feed_version_id=NEW.id AND ((t.gtfs_direction_id IS NOT NULL AND p.gtfs_direction_id IS NOT NULL AND t.gtfs_direction_id<>p.gtfs_direction_id)
       OR (t.shape_id IS NOT NULL AND p.shape_id IS NOT NULL AND t.shape_id<>p.shape_id))) THEN
   RAISE EXCEPTION 'Trip direction or shape differs from pattern' USING ERRCODE='23514';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM gtfs.agencies WHERE feed_version_id=NEW.id)
     OR NOT EXISTS (SELECT 1 FROM gtfs.routes WHERE feed_version_id=NEW.id)
     OR NOT EXISTS (SELECT 1 FROM gtfs.stops WHERE feed_version_id=NEW.id)
     OR NOT EXISTS (SELECT 1 FROM gtfs.trips WHERE feed_version_id=NEW.id)
     OR NOT EXISTS (SELECT 1 FROM gtfs.stop_times WHERE feed_version_id=NEW.id) THEN
   RAISE EXCEPTION 'Schedule baseline is empty' USING ERRCODE = '23514';
  END IF;
  NEW.validated_at := CURRENT_TIMESTAMP;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER feed_transition_guard BEFORE INSERT OR UPDATE OR DELETE ON ingest.feed_versions
 FOR EACH ROW EXECUTE FUNCTION ingest.guard_feed_transition();

CREATE FUNCTION ingest.guard_validation_write() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE v_id bigint; v_state text;
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'Validation evidence is append-only' USING ERRCODE = '55000'; END IF;
 v_id := NEW.feed_version_id;
 SELECT state INTO v_state FROM ingest.feed_versions WHERE id=v_id FOR SHARE;
 IF v_state IS DISTINCT FROM 'validating' THEN RAISE EXCEPTION 'Validation requires validating state' USING ERRCODE = '55000'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_write_guard BEFORE INSERT OR UPDATE OR DELETE ON ingest.validation_results
 FOR EACH ROW EXECUTE FUNCTION ingest.guard_validation_write();

CREATE FUNCTION ingest.guard_publication() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
 IF NEW.active_feed_version_id IS NOT NULL AND NOT EXISTS
   (SELECT 1 FROM ingest.feed_versions WHERE id=NEW.active_feed_version_id AND feed_source_id=NEW.feed_source_id AND state='ready') THEN
  RAISE EXCEPTION 'Only ready feeds can be published' USING ERRCODE = '23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER publication_ready_guard BEFORE INSERT OR UPDATE ON ingest.feed_publications
 FOR EACH ROW EXECUTE FUNCTION ingest.guard_publication();

CREATE FUNCTION ingest.promote_feed(p_source bigint,p_version bigint,p_expected_generation bigint,p_actor text,p_reason text)
 RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_old bigint; v_generation bigint;
BEGIN
 IF nullif(btrim(p_actor),'') IS NULL OR nullif(btrim(p_reason),'') IS NULL OR p_expected_generation IS NULL OR p_version IS NULL THEN
  RAISE EXCEPTION 'Actor, reason, version and expected generation are required' USING ERRCODE='22023';
 END IF;
 INSERT INTO ingest.feed_publications(feed_source_id) VALUES(p_source) ON CONFLICT DO NOTHING;
 SELECT active_feed_version_id,generation INTO v_old,v_generation FROM ingest.feed_publications WHERE feed_source_id=p_source FOR UPDATE;
 IF v_generation <> p_expected_generation THEN RAISE EXCEPTION 'Publication generation conflict' USING ERRCODE='40001'; END IF;
 IF NOT EXISTS (SELECT 1 FROM ingest.feed_versions WHERE id=p_version AND feed_source_id=p_source AND state='ready') THEN
  RAISE EXCEPTION 'Version is not ready for this source' USING ERRCODE='23514';
 END IF;
 IF v_old = p_version THEN RETURN v_generation; END IF;
 UPDATE ingest.feed_publications SET active_feed_version_id=p_version,generation=generation+1,updated_at=CURRENT_TIMESTAMP WHERE feed_source_id=p_source;
 INSERT INTO ingest.feed_promotion_events(feed_source_id,previous_version_id,new_version_id,generation,actor_reference,reason)
  VALUES(p_source,v_old,p_version,v_generation+1,p_actor,p_reason);
 RETURN v_generation+1;
END $$;
REVOKE ALL ON FUNCTION ingest.promote_feed(bigint,bigint,bigint,text,text) FROM PUBLIC;

-- Group roles contain no credentials. A future deployment may grant membership to separate login roles.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mobility_app') THEN CREATE ROLE mobility_app NOLOGIN; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mobility_importer') THEN CREATE ROLE mobility_importer NOLOGIN; END IF;
END $$;
GRANT USAGE ON SCHEMA public,gtfs,ingest TO mobility_app,mobility_importer;
GRANT SELECT,INSERT,UPDATE,DELETE ON public.users,public.password_reset_tokens,public.sessions,public.personal_access_tokens,
 public.favorites,public.cache,public.cache_locks,public.jobs,public.job_batches,public.failed_jobs TO mobility_app;
GRANT SELECT ON public.lines,public.bus_stops,public.bus_stop_line,public.stop_source_references,public.line_route_mappings,public.line_stop_sources TO mobility_app;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO mobility_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA gtfs TO mobility_importer;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA gtfs,ingest TO mobility_importer;
GRANT SELECT,INSERT,UPDATE ON ingest.feed_sources,ingest.feed_versions,ingest.import_runs TO mobility_importer;
GRANT SELECT,INSERT ON ingest.import_rejections,ingest.validation_results TO mobility_importer;
GRANT SELECT ON ingest.feed_publications,ingest.feed_promotion_events TO mobility_importer,mobility_app;
GRANT EXECUTE ON FUNCTION ingest.promote_feed(bigint,bigint,bigint,text,text) TO mobility_importer;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['agencies','routes','stops','services','calendars','service_exceptions','shapes','shape_points','journey_patterns','journey_pattern_stops','trips','stop_times','frequencies'] LOOP
  EXECUTE format('CREATE VIEW gtfs.active_%I AS SELECT t.* FROM gtfs.%I t JOIN ingest.feed_publications p ON p.active_feed_version_id=t.feed_version_id',t,t);
  EXECUTE format('GRANT SELECT ON gtfs.active_%I TO mobility_app',t);
 END LOOP;
END $$;
