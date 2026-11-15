-- Explicit provisioning only, on the target PostgreSQL database. Never executed by startup.
-- The role has no login/password and no authentication/user-table privileges.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='mobility_processor_catalog') THEN
  CREATE ROLE mobility_processor_catalog NOLOGIN;
 END IF;
END $$;
GRANT USAGE ON SCHEMA public,gtfs,ingest TO mobility_processor_catalog;
GRANT SELECT,INSERT,UPDATE ON public.lines,public.bus_stops,public.stop_source_references,
 public.bus_stop_line,public.line_stop_sources,public.line_route_mappings TO mobility_processor_catalog;
GRANT USAGE,SELECT ON SEQUENCE public.lines_id_seq,public.bus_stops_id_seq,
 public.stop_source_references_id_seq,public.bus_stop_line_id_seq,
 public.line_stop_sources_id_seq,public.line_route_mappings_id_seq TO mobility_processor_catalog;
GRANT SELECT ON gtfs.routes,gtfs.journey_patterns,gtfs.shapes,ingest.feed_versions TO mobility_processor_catalog;
