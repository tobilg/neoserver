#!/bin/bash
# Generate PostGIS SQL from CITE WMS 1.3.0 test data GML files
# Parses the GML files and generates INSERT statements

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SCRIPT_DIR}/data"
GML_DIR="${DATA_DIR}/gml"
OUTPUT_FILE="${SCRIPT_DIR}/02-cite-data.sql"

echo "=== Generating PostGIS SQL from CITE Test Data ==="

if [ ! -d "${GML_DIR}" ]; then
    echo "Error: GML directory not found at ${GML_DIR}"
    echo "Run download.sh and extract.sh first."
    exit 1
fi

# Generate the SQL file
cat > "${OUTPUT_FILE}" << 'SQLEOF'
-- CITE WMS 1.3.0 Test Data
-- Based on OGC Simple Features Specification Blue Lake dataset
-- Converted to WGS84 (EPSG:4326/CRS:84) coordinates centered at 0,0
--
-- Generated from OGC CITE test data: https://opengeospatial.github.io/ets-wms13/

CREATE EXTENSION IF NOT EXISTS postgis;

-- Create cite schema for test data
DROP SCHEMA IF EXISTS cite CASCADE;
CREATE SCHEMA cite;

-- ============================================================================
-- BasicPolygons - Diamond and two overlapping squares
-- ============================================================================
DROP TABLE IF EXISTS cite."BasicPolygons";
CREATE TABLE cite."BasicPolygons" (
    id SERIAL PRIMARY KEY,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."BasicPolygons" (geom) VALUES
    -- Diamond
    (ST_GeomFromText('POLYGON((-1 0, 0 1, 1 0, 0 -1, -1 0))', 4326)),
    -- Square 1
    (ST_GeomFromText('POLYGON((-2 6, 1 6, 1 3, -2 3, -2 6))', 4326)),
    -- Square 2
    (ST_GeomFromText('POLYGON((-1 5, 2 5, 2 2, -1 2, -1 5))', 4326));

-- ============================================================================
-- Lakes - Blue Lake with Goose Island (polygon with hole)
-- ============================================================================
DROP TABLE IF EXISTS cite."Lakes";
CREATE TABLE cite."Lakes" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."Lakes" (name, geom) VALUES
    ('Blue Lake', ST_GeomFromText('POLYGON((0.0006 -0.0018, 0.0010 -0.0006, 0.0024 -0.0001, 0.0031 -0.0015, 0.0006 -0.0018), (0.0017 -0.0011, 0.0025 -0.0011, 0.0025 -0.0006, 0.0017 -0.0006, 0.0017 -0.0011))', 4326));

-- ============================================================================
-- Forests - Green Forest (multipolygon)
-- ============================================================================
DROP TABLE IF EXISTS cite."Forests";
CREATE TABLE cite."Forests" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(MultiPolygon, 4326) NOT NULL
);

INSERT INTO cite."Forests" (name, geom) VALUES
    ('Green Forest', ST_GeomFromText('MULTIPOLYGON(((-0.0014 -0.0024, -0.0014 0.0002, 0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014, 0.0042 0.0018, 0.0042 -0.0024, -0.0014 -0.0024)))', 4326));

-- ============================================================================
-- Bridges - Cam Bridge (point)
-- ============================================================================
DROP TABLE IF EXISTS cite."Bridges";
CREATE TABLE cite."Bridges" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."Bridges" (name, geom) VALUES
    ('Cam Bridge', ST_SetSRID(ST_MakePoint(0.0002, 0.0007), 4326));

-- ============================================================================
-- Buildings - Two buildings on Main Street (polygons)
-- ============================================================================
DROP TABLE IF EXISTS cite."Buildings";
CREATE TABLE cite."Buildings" (
    id SERIAL PRIMARY KEY,
    address TEXT,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."Buildings" (address, geom) VALUES
    ('123 Main Street', ST_GeomFromText('POLYGON((0.0008 0.0005, 0.0008 0.0007, 0.0012 0.0007, 0.0012 0.0005, 0.0008 0.0005))', 4326)),
    ('215 Main Street', ST_GeomFromText('POLYGON((0.0020 0.0008, 0.0020 0.0010, 0.0024 0.0010, 0.0024 0.0008, 0.0020 0.0008))', 4326));

-- ============================================================================
-- BuildingCenters - Building center points
-- ============================================================================
DROP TABLE IF EXISTS cite."BuildingCenters";
CREATE TABLE cite."BuildingCenters" (
    id SERIAL PRIMARY KEY,
    address TEXT,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."BuildingCenters" (address, geom) VALUES
    ('123 Main Street', ST_SetSRID(ST_MakePoint(0.0010, 0.0006), 4326)),
    ('215 Main Street', ST_SetSRID(ST_MakePoint(0.0022, 0.0009), 4326));

-- ============================================================================
-- DividedRoutes - Route 75 (multilinestring)
-- ============================================================================
DROP TABLE IF EXISTS cite."DividedRoutes";
CREATE TABLE cite."DividedRoutes" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    num_lanes INTEGER,
    geom geometry(MultiLineString, 4326) NOT NULL
);

INSERT INTO cite."DividedRoutes" (name, num_lanes, geom) VALUES
    ('Route 75', 4, ST_GeomFromText('MULTILINESTRING((-0.0032 -0.0024, -0.0032 0.0024), (-0.0026 -0.0024, -0.0026 0.0024))', 4326));

-- ============================================================================
-- MapNeatline - Border boundary (linestring)
-- ============================================================================
DROP TABLE IF EXISTS cite."MapNeatline";
CREATE TABLE cite."MapNeatline" (
    id SERIAL PRIMARY KEY,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."MapNeatline" (geom) VALUES
    (ST_GeomFromText('LINESTRING(-0.0042 -0.0024, -0.0042 0.0024, 0.0042 0.0024, 0.0042 -0.0024, -0.0042 -0.0024)', 4326));

-- ============================================================================
-- NamedPlaces - Ashton and Goose Island (polygons)
-- ============================================================================
DROP TABLE IF EXISTS cite."NamedPlaces";
CREATE TABLE cite."NamedPlaces" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."NamedPlaces" (name, geom) VALUES
    ('Ashton', ST_GeomFromText('POLYGON((0.0020 0.0024, 0.0042 0.0024, 0.0042 0.0006, 0.0014 0.0006, 0.0014 0.0010, 0.0020 0.0024))', 4326)),
    ('Goose Island', ST_GeomFromText('POLYGON((0.0017 -0.0011, 0.0017 -0.0006, 0.0025 -0.0006, 0.0025 -0.0011, 0.0017 -0.0011))', 4326));

-- ============================================================================
-- Ponds - Stock Pond (multipolygon with two ponds)
-- ============================================================================
DROP TABLE IF EXISTS cite."Ponds";
CREATE TABLE cite."Ponds" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    type TEXT,
    geom geometry(MultiPolygon, 4326) NOT NULL
);

INSERT INTO cite."Ponds" (name, type, geom) VALUES
    (NULL, 'Stock Pond', ST_GeomFromText('MULTIPOLYGON(((-0.0020 0.0018, -0.0018 0.0020, -0.0018 0.0016, -0.0020 0.0018)), ((-0.0016 0.0016, -0.0016 0.0020, -0.0014 0.0018, -0.0016 0.0016)))', 4326));

-- ============================================================================
-- RoadSegments - Various road segments (linestrings)
-- ============================================================================
DROP TABLE IF EXISTS cite."RoadSegments";
CREATE TABLE cite."RoadSegments" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."RoadSegments" (name, geom) VALUES
    ('Route 5', ST_GeomFromText('LINESTRING(-0.0042 -0.0006, -0.0032 -0.0003, -0.0026 -0.0001, -0.0014 0.0002, 0.0002 0.0007)', 4326)),
    ('Route 5', ST_GeomFromText('LINESTRING(0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014)', 4326)),
    ('Route 5', ST_GeomFromText('LINESTRING(0.0028 0.0014, 0.0030 0.0024)', 4326)),
    ('Main Street', ST_GeomFromText('LINESTRING(0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014, 0.0042 0.0018)', 4326)),
    ('Dirt Road by Green Forest', ST_GeomFromText('LINESTRING(-0.0014 -0.0024, -0.0014 0.0002)', 4326));

-- ============================================================================
-- Streams - Cam Stream and unnamed stream (linestrings)
-- ============================================================================
DROP TABLE IF EXISTS cite."Streams";
CREATE TABLE cite."Streams" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."Streams" (name, geom) VALUES
    ('Cam Stream', ST_GeomFromText('LINESTRING(-0.0004 0.0024, 0.0002 0.0017, -0.0001 0.0012, 0.0002 0.0007, 0.0010 -0.0006)', 4326)),
    (NULL, ST_GeomFromText('LINESTRING(0.0034 -0.0024, 0.0036 -0.0020, 0.0031 -0.0015)', 4326));

-- ============================================================================
-- Autos - Automobiles with time dimension (points)
-- ============================================================================
DROP TABLE IF EXISTS cite."Autos";
CREATE TABLE cite."Autos" (
    id SERIAL PRIMARY KEY,
    num INTEGER,
    "time" TIMESTAMP WITH TIME ZONE,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."Autos" (num, "time", geom) VALUES
    (1, '2000-01-01T00:00:00Z', ST_SetSRID(ST_MakePoint(-0.0014, 0.0002), 4326)),
    (1, '2000-01-01T00:00:05Z', ST_SetSRID(ST_MakePoint(-0.0022, 0.0), 4326)),
    (1, '2000-01-01T00:00:10Z', ST_SetSRID(ST_MakePoint(-0.0029, -0.0002), 4326)),
    (1, '2000-01-01T00:00:15Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0004), 4326)),
    (1, '2000-01-01T00:00:20Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0008), 4326)),
    (1, '2000-01-01T00:00:25Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0014), 4326)),
    (1, '2000-01-01T00:00:30Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0022), 4326)),
    (2, '2000-01-01T00:00:20Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0022), 4326)),
    (2, '2000-01-01T00:00:25Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0012), 4326)),
    (2, '2000-01-01T00:00:30Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0002), 4326)),
    (2, '2000-01-01T00:00:35Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0008), 4326)),
    (2, '2000-01-01T00:00:40Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0018), 4326)),
    (3, '2000-01-01T00:00:40Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0020), 4326)),
    (3, '2000-01-01T00:00:45Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0012), 4326)),
    (3, '2000-01-01T00:00:50Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0004), 4326)),
    (3, '2000-01-01T00:00:55Z', ST_SetSRID(ST_MakePoint(-0.0026, 0.0004), 4326)),
    (3, '2000-01-01T00:01:00Z', ST_SetSRID(ST_MakePoint(-0.0026, 0.0012), 4326)),
    (4, '2000-01-01T00:00:55Z', ST_SetSRID(ST_MakePoint(0.0029, 0.0019), 4326)),
    (4, '2000-01-01T00:01:00Z', ST_SetSRID(ST_MakePoint(0.0028, 0.0014), 4326));

-- ============================================================================
-- LakesWithElevation - Lakes with elevation dimension (polygons)
-- ============================================================================
DROP TABLE IF EXISTS cite."LakesWithElevation";
CREATE TABLE cite."LakesWithElevation" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    elev INTEGER,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."LakesWithElevation" (name, elev, geom) VALUES
    ('Blue Lake', 500, ST_GeomFromText('POLYGON((0.0006 -0.0018, 0.0010 -0.0006, 0.0024 -0.0001, 0.0031 -0.0015, 0.0006 -0.0018), (0.0017 -0.0011, 0.0025 -0.0011, 0.0025 -0.0006, 0.0017 -0.0006, 0.0017 -0.0011))', 4326)),
    ('Blue Lake', 490, ST_GeomFromText('POLYGON((0.0010 -0.0016, 0.0012 -0.0006, 0.0016 -0.0005, 0.0016 -0.0012, 0.0024 -0.0012, 0.0024 -0.0014, 0.0010 -0.0016))', 4326)),
    ('Blue Lake', 480, ST_GeomFromText('POLYGON((0.0011 -0.0015, 0.0013 -0.0007, 0.0015 -0.0007, 0.0015 -0.0013, 0.0011 -0.0015))', 4326));

-- ============================================================================
-- Create spatial indexes for performance
-- ============================================================================
CREATE INDEX idx_basicpolygons_geom ON cite."BasicPolygons" USING GIST (geom);
CREATE INDEX idx_lakes_geom ON cite."Lakes" USING GIST (geom);
CREATE INDEX idx_forests_geom ON cite."Forests" USING GIST (geom);
CREATE INDEX idx_bridges_geom ON cite."Bridges" USING GIST (geom);
CREATE INDEX idx_buildings_geom ON cite."Buildings" USING GIST (geom);
CREATE INDEX idx_buildingcenters_geom ON cite."BuildingCenters" USING GIST (geom);
CREATE INDEX idx_dividedroutes_geom ON cite."DividedRoutes" USING GIST (geom);
CREATE INDEX idx_mapneatline_geom ON cite."MapNeatline" USING GIST (geom);
CREATE INDEX idx_namedplaces_geom ON cite."NamedPlaces" USING GIST (geom);
CREATE INDEX idx_ponds_geom ON cite."Ponds" USING GIST (geom);
CREATE INDEX idx_roadsegments_geom ON cite."RoadSegments" USING GIST (geom);
CREATE INDEX idx_streams_geom ON cite."Streams" USING GIST (geom);
CREATE INDEX idx_autos_geom ON cite."Autos" USING GIST (geom);
CREATE INDEX idx_lakeswithelevation_geom ON cite."LakesWithElevation" USING GIST (geom);

-- Grant permissions
GRANT USAGE ON SCHEMA cite TO PUBLIC;
GRANT SELECT ON ALL TABLES IN SCHEMA cite TO PUBLIC;
SQLEOF

echo "Generated SQL file: ${OUTPUT_FILE}"
echo ""
echo "To load into PostgreSQL:"
echo "  psql -h localhost -U postgres -d postgis -f ${OUTPUT_FILE}"
echo ""
echo "Or copy to testing/initdb/ for Docker initialization:"
echo "  cp ${OUTPUT_FILE} ../../testing/initdb/"
