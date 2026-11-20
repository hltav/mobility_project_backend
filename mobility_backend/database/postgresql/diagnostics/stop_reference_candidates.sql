-- Read-only. Named parameters work with Laravel bindings and psql -v.
-- No automatic publication, confirmation or interpretation of numeric IDs.
WITH args AS (
 SELECT CAST(:feed_version_id AS bigint) feed_id, CAST(:pattern_id AS bigint) pattern_id,
        CAST(:line_id AS bigint) line_id, CAST(:radius_m AS double precision) radius_m
), context AS (
 SELECT a.*, p.route_id, p.gtfs_direction_id, l.sptrans_line_id, l.sptrans_direction,
        src.provider AS gtfs_provider, v.checksum_sha256,
        EXISTS (SELECT 1 FROM public.line_route_mappings m
          WHERE m.line_id=l.id AND m.feed_version_id=p.feed_version_id
            AND m.route_id=p.route_id AND m.pattern_id=p.id
            AND m.gtfs_direction_id IS NOT DISTINCT FROM p.gtfs_direction_id
            AND m.status='confirmed'
            AND (m.valid_from IS NULL OR m.valid_from<=CURRENT_TIMESTAMP)
            AND (m.valid_until IS NULL OR m.valid_until>CURRENT_TIMESTAMP)) pattern_mapping_confirmed,
        EXISTS (SELECT 1 FROM ingest.feed_publications pub
          WHERE pub.active_feed_version_id=v.id) feed_published
 FROM args a JOIN gtfs.journey_patterns p ON p.feed_version_id=a.feed_id AND p.id=a.pattern_id
 JOIN ingest.feed_versions v ON v.id=p.feed_version_id AND v.state='ready'
 JOIN ingest.feed_sources src ON src.id=v.feed_source_id AND src.source_kind='gtfs_schedule'
 JOIN public.lines l ON l.id=a.line_id AND l.provider='sptrans'
   AND l.sptrans_line_id IS NOT NULL AND l.sptrans_direction IN (1,2)
 WHERE a.radius_m>0 AND a.radius_m<=200
), candidates AS (
 SELECT c.*, ps.pattern_sequence, s.id AS gtfs_stop_pk, s.gtfs_stop_id AS external_stop_id,
        s.stop_code, s.name AS gtfs_stop_name, s.location_type, s.parent_gtfs_stop_pk,
        old.id AS existing_reference_id, old.status AS existing_reference_status,
        old.provider=c.gtfs_provider AS existing_provider_matches,
        old.stop_id AS existing_canonical_stop_id,
        hit.canonical_stop_id, hit.sptrans_stop_code, hit.sptrans_reference_id,
        hit.sptrans_stop_name, hit.sptrans_reference_method, hit.distance_m,
        hit.id_equal, hit.code_equal, hit.names_equal, hit.within_radius,
        hit.line_compatible, hit.opposite_direction_served
 FROM context c
 JOIN gtfs.journey_pattern_stops ps ON ps.feed_version_id=c.feed_id AND ps.pattern_id=c.pattern_id
 JOIN gtfs.stops s ON s.feed_version_id=ps.feed_version_id AND s.id=ps.gtfs_stop_pk
 LEFT JOIN public.stop_source_references old ON old.source_kind='gtfs'
   AND old.feed_version_id=s.feed_version_id AND old.gtfs_stop_pk=s.id
 LEFT JOIN LATERAL (
   SELECT b.id AS canonical_stop_id, sr.sptrans_stop_code, sr.id AS sptrans_reference_id,
          b.name AS sptrans_stop_name, sr.method AS sptrans_reference_method,
          public.ST_Distance(s.position_geog,b.position_geog) AS distance_m,
          s.gtfs_stop_id=sr.external_stop_id AS id_equal,
          COALESCE(s.stop_code=sr.external_stop_id,false) AS code_equal,
          lower(trim(s.name))=lower(trim(b.name)) AS names_equal,
          COALESCE(public.ST_DWithin(s.position_geog,b.position_geog,c.radius_m),false) AS within_radius,
          EXISTS (SELECT 1 FROM public.line_stop_sources ls
            WHERE ls.stop_id=b.id AND ls.line_id=c.line_id AND ls.source='sptrans'
              AND ls.status='confirmed' AND ls.feed_version_id IS NULL
              AND ls.external_line_id=c.sptrans_line_id::text
              AND ls.external_stop_id=sr.external_stop_id) AS line_compatible,
          EXISTS (SELECT 1 FROM public.line_stop_sources ls JOIN public.lines other ON other.id=ls.line_id
            WHERE ls.stop_id=b.id AND ls.source='sptrans' AND ls.status='confirmed'
              AND ls.feed_version_id IS NULL AND ls.external_stop_id=sr.external_stop_id
              AND other.provider='sptrans' AND other.sptrans_line_id::text=ls.external_line_id
              AND other.display_code=(SELECT display_code FROM public.lines WHERE id=c.line_id)
              AND other.sptrans_direction<>c.sptrans_direction) AS opposite_direction_served
   FROM public.bus_stops b JOIN public.stop_source_references sr ON sr.stop_id=b.id
     AND sr.provider='sptrans' AND sr.source_kind='sptrans' AND sr.status='confirmed'
   WHERE public.ST_DWithin(s.position_geog,b.position_geog,c.radius_m)
      OR s.gtfs_stop_id=sr.external_stop_id OR s.stop_code=sr.external_stop_id OR old.stop_id=b.id
 ) hit ON true
), counted AS (
 SELECT *, count(*) FILTER (WHERE line_compatible AND within_radius AND location_type=0)
   OVER (PARTITION BY feed_id,pattern_id,pattern_sequence) AS eligible_count
 FROM candidates
)
SELECT feed_id AS feed_version_id, pattern_id, route_id, gtfs_direction_id, line_id,
       sptrans_line_id, sptrans_direction, gtfs_provider, checksum_sha256,
       gtfs_stop_pk, external_stop_id, stop_code, gtfs_stop_name, pattern_sequence,
       parent_gtfs_stop_pk, canonical_stop_id, sptrans_stop_code, sptrans_stop_name,
       sptrans_reference_id, sptrans_reference_method, distance_m, radius_m,
       existing_reference_id, existing_reference_status,
       jsonb_build_object('canonical_stop_id',canonical_stop_id,
         'existing_canonical_stop_id',existing_canonical_stop_id,
         'existing_reference_status',existing_reference_status,
         'existing_provider_matches',existing_provider_matches,
         'id_equal',id_equal,'code_equal',code_equal,'names_equal',names_equal,
         'within_radius',within_radius,'line_compatible',line_compatible,
         'opposite_direction_served',opposite_direction_served,
         'location_type',location_type,'eligible_count',eligible_count,
         'pattern_mapping_confirmed',pattern_mapping_confirmed,'feed_published',feed_published) AS evidence
FROM counted ORDER BY pattern_sequence, distance_m NULLS LAST, sptrans_stop_code;
