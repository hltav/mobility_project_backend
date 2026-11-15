CREATE EXTENSION IF NOT EXISTS postgis WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public;
CREATE SCHEMA ingest;
CREATE SCHEMA gtfs;
-- SECURITY DEFINER publication functions must not resolve objects in user-writable schemas.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA ingest, gtfs FROM PUBLIC;
