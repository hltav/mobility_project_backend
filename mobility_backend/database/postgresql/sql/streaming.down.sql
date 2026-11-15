-- Do not erase recovery history in a populated deployment.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM ingest.feed_recovery_events) OR EXISTS(SELECT 1 FROM ingest.feed_versions WHERE profile_version='open-transit.schedule.stage3.v1') THEN RAISE EXCEPTION 'Stage 3 data/history present; rollback requires an explicit operational plan'; END IF; END $$;
DROP FUNCTION ingest.recover_feed(bigint,text,text,text,text,text);
DROP TABLE ingest.feed_recovery_events;
CREATE OR REPLACE FUNCTION ingest.guard_feed_transition() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
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
DROP FUNCTION ingest.assert_snapshot_counts(bigint,jsonb);
