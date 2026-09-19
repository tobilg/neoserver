-- CITE WMS 1.3.0 Test Data
-- Based on OGC Simple Features Specification Blue Lake dataset
-- Converted to WGS84 (EPSG:4326/CRS:84) coordinates centered at 0,0

CREATE EXTENSION IF NOT EXISTS postgis;

-- Create cite schema for test data
DROP SCHEMA IF EXISTS cite CASCADE;
CREATE SCHEMA cite;

-- ============================================================================
-- BasicPolygons - Diamond and two overlapping squares
-- Added 'name' column for PropertyIsLike tests
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."BasicPolygons";
CREATE TABLE cite."BasicPolygons" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    area_sqm DOUBLE PRECISION,
    population INTEGER,
    created_date TIMESTAMP WITH TIME ZONE,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."BasicPolygons" (name, description, identifier, area_sqm, population, created_date, geom) VALUES
    -- Diamond has NULL population for PropertyIsNil tests
    ('Diamond', 'A diamond shape', 'polygon-001', 2.0, NULL, '1999-01-01T00:00:00Z', ST_GeomFromText('POLYGON((-1 0, 0 1, 1 0, 0 -1, -1 0))', 4326)),
    -- Square 1 has NULL area for PropertyIsNil tests
    ('Square1', 'First square', 'polygon-002', NULL, 250, '1999-06-15T12:00:00Z', ST_GeomFromText('POLYGON((-2 6, 1 6, 1 3, -2 3, -2 6))', 4326)),
    -- Square 2 has NULL created_date for PropertyIsNil tests
    ('Square2', 'Second square', 'polygon-003', 9.0, 175, NULL, ST_GeomFromText('POLYGON((-1 5, 2 5, 2 2, -1 2, -1 5))', 4326)),
    -- Fourth polygon has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 1.0, 50, '2000-03-20T16:45:00Z', ST_GeomFromText('POLYGON((3 3, 4 3, 4 4, 3 4, 3 3))', 4326));

-- ============================================================================
-- Lakes - Blue Lake with Goose Island (polygon with hole)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Lakes";
CREATE TABLE cite."Lakes" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    depth_m DOUBLE PRECISION,
    fish_count INTEGER,
    last_survey TIMESTAMP WITH TIME ZONE,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."Lakes" (name, description, identifier, depth_m, fish_count, last_survey, geom) VALUES
    -- Blue Lake has NULL fish_count for PropertyIsNil tests
    ('Blue Lake', 'A beautiful blue lake', 'lake-001', 15.5, NULL, '1999-08-15T10:00:00Z', ST_GeomFromText('POLYGON((0.0006 -0.0018, 0.0010 -0.0006, 0.0024 -0.0001, 0.0031 -0.0015, 0.0006 -0.0018), (0.0017 -0.0011, 0.0025 -0.0011, 0.0025 -0.0006, 0.0017 -0.0006, 0.0017 -0.0011))', 4326)),
    -- Second lake has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 3.2, 200, '2000-02-20T14:30:00Z', ST_GeomFromText('POLYGON((0.0035 -0.0020, 0.0038 -0.0018, 0.0038 -0.0022, 0.0035 -0.0020))', 4326));

-- ============================================================================
-- Forests - Green Forest (multipolygon)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Forests";
CREATE TABLE cite."Forests" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    tree_count INTEGER,
    area_hectares DOUBLE PRECISION,
    last_survey TIMESTAMP WITH TIME ZONE,
    geom geometry(MultiPolygon, 4326) NOT NULL
);

INSERT INTO cite."Forests" (name, description, identifier, tree_count, area_hectares, last_survey, geom) VALUES
    -- Green Forest has NULL tree_count for PropertyIsNil tests
    ('Green Forest', 'A large green forest', 'forest-001', NULL, 125.5, '1998-04-15T00:00:00Z', ST_GeomFromText('MULTIPOLYGON(((-0.0014 -0.0024, -0.0014 0.0002, 0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014, 0.0042 0.0018, 0.0042 -0.0024, -0.0014 -0.0024)))', 4326)),
    -- Second forest has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 5000, 12.0, '1999-10-20T00:00:00Z', ST_GeomFromText('MULTIPOLYGON(((0.0030 0.0020, 0.0035 0.0020, 0.0035 0.0015, 0.0030 0.0015, 0.0030 0.0020)))', 4326));

-- ============================================================================
-- Bridges - Cam Bridge (point)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Bridges";
CREATE TABLE cite."Bridges" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    span_m DOUBLE PRECISION,
    weight_limit_kg INTEGER,
    built_date TIMESTAMP WITH TIME ZONE,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."Bridges" (name, description, identifier, span_m, weight_limit_kg, built_date, geom) VALUES
    -- First bridge has NULL weight_limit for PropertyIsNil tests
    ('Cam Bridge', 'Historic stone bridge', 'bridge-001', 25.5, NULL, '1985-06-01T00:00:00Z', ST_SetSRID(ST_MakePoint(0.0002, 0.0007), 4326)),
    -- Second bridge has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 15.0, 25000, '1999-11-15T00:00:00Z', ST_SetSRID(ST_MakePoint(0.0015, 0.0012), 4326));

-- ============================================================================
-- Buildings - Two buildings on Main Street (polygons)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Buildings";
CREATE TABLE cite."Buildings" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    address TEXT,
    floors INTEGER,
    area_sqm DOUBLE PRECISION,
    constructed TIMESTAMP WITH TIME ZONE,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."Buildings" (name, description, identifier, address, floors, area_sqm, constructed, geom) VALUES
    -- First building has NULL floors for PropertyIsNil tests
    ('Building A', 'A commercial building', 'building-001', '123 Main Street', NULL, 450.5, '1975-03-15T00:00:00Z', ST_GeomFromText('POLYGON((0.0008 0.0005, 0.0008 0.0007, 0.0012 0.0007, 0.0012 0.0005, 0.0008 0.0005))', 4326)),
    -- Second building has NULL area for PropertyIsNil tests
    ('Building B', 'A residential building', 'building-002', '215 Main Street', 2, NULL, '1988-09-22T00:00:00Z', ST_GeomFromText('POLYGON((0.0020 0.0008, 0.0020 0.0010, 0.0024 0.0010, 0.0024 0.0008, 0.0020 0.0008))', 4326)),
    -- Third building has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, '330 Main Street', 1, 150.0, '1999-12-01T00:00:00Z', ST_GeomFromText('POLYGON((0.0030 0.0005, 0.0030 0.0007, 0.0034 0.0007, 0.0034 0.0005, 0.0030 0.0005))', 4326));

-- ============================================================================
-- BuildingCenters - Building center points
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."BuildingCenters";
CREATE TABLE cite."BuildingCenters" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    address TEXT,
    floors INTEGER,
    height_m DOUBLE PRECISION,
    built_date TIMESTAMP WITH TIME ZONE,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."BuildingCenters" (name, description, identifier, address, floors, height_m, built_date, geom) VALUES
    -- First has NULL height for PropertyIsNil tests
    ('Center A', 'Center of building A', 'center-001', '123 Main Street', 3, NULL, '1975-03-15T00:00:00Z', ST_SetSRID(ST_MakePoint(0.0010, 0.0006), 4326)),
    -- Second has NULL floors for PropertyIsNil tests
    ('Center B', 'Center of building B', 'center-002', '215 Main Street', NULL, 8.0, '1988-09-22T00:00:00Z', ST_SetSRID(ST_MakePoint(0.0022, 0.0009), 4326)),
    -- Third has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, '330 Main Street', 1, 4.5, '1999-12-01T00:00:00Z', ST_SetSRID(ST_MakePoint(0.0032, 0.0006), 4326));

-- ============================================================================
-- DividedRoutes - Route 75 (multilinestring)
-- Added temporal column for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."DividedRoutes";
CREATE TABLE cite."DividedRoutes" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    num_lanes INTEGER,
    length_km DOUBLE PRECISION,
    paved_date TIMESTAMP WITH TIME ZONE,
    geom geometry(MultiLineString, 4326) NOT NULL
);

INSERT INTO cite."DividedRoutes" (name, description, identifier, num_lanes, length_km, paved_date, geom) VALUES
    -- Route 75 has NULL num_lanes for PropertyIsNil tests
    ('Route 75', 'Major highway', 'route-001', NULL, 8.5, '1990-05-20T00:00:00Z', ST_GeomFromText('MULTILINESTRING((-0.0032 -0.0024, -0.0032 0.0024), (-0.0026 -0.0024, -0.0026 0.0024))', 4326)),
    -- Second route has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 2, 4.2, '1999-08-10T00:00:00Z', ST_GeomFromText('MULTILINESTRING((-0.0040 -0.0020, -0.0040 0.0020), (-0.0038 -0.0020, -0.0038 0.0020))', 4326));

-- ============================================================================
-- MapNeatline - Border boundary (linestring)
-- Added 'name' column for PropertyIsLike tests
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."MapNeatline";
CREATE TABLE cite."MapNeatline" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    scale INTEGER,
    length_km DOUBLE PRECISION,
    created_date TIMESTAMP WITH TIME ZONE,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."MapNeatline" (name, description, identifier, scale, length_km, created_date, geom) VALUES
    -- Map Border has NULL scale for PropertyIsNil tests
    ('Map Border', 'Primary map boundary', 'neatline-001', NULL, 25.6, '1995-01-01T00:00:00Z', ST_GeomFromText('LINESTRING(-0.0042 -0.0024, -0.0042 0.0024, 0.0042 0.0024, 0.0042 -0.0024, -0.0042 -0.0024)', 4326)),
    -- Second neatline has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 100000, 32.0, '1999-06-15T00:00:00Z', ST_GeomFromText('LINESTRING(-0.0050 -0.0030, -0.0050 0.0030, 0.0050 0.0030, 0.0050 -0.0030, -0.0050 -0.0030)', 4326));

-- ============================================================================
-- NamedPlaces - Ashton and Goose Island (polygons)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."NamedPlaces";
CREATE TABLE cite."NamedPlaces" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    population INTEGER,
    area_sqkm DOUBLE PRECISION,
    founded_date TIMESTAMP WITH TIME ZONE,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."NamedPlaces" (name, description, identifier, population, area_sqkm, founded_date, geom) VALUES
    -- Ashton has NULL population for PropertyIsNil tests
    ('Ashton', 'Historic town center', 'place-001', NULL, 8.5, '1850-07-04T00:00:00Z', ST_GeomFromText('POLYGON((0.0020 0.0024, 0.0042 0.0024, 0.0042 0.0006, 0.0014 0.0006, 0.0014 0.0010, 0.0020 0.0024))', 4326)),
    -- Goose Island has NULL area for PropertyIsNil tests
    ('Goose Island', 'Small island', 'place-002', 50, NULL, '1920-03-15T00:00:00Z', ST_GeomFromText('POLYGON((0.0017 -0.0011, 0.0017 -0.0006, 0.0025 -0.0006, 0.0025 -0.0011, 0.0017 -0.0011))', 4326)),
    -- Third place has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 100, 0.2, '1975-01-01T00:00:00Z', ST_GeomFromText('POLYGON((-0.0030 0.0010, -0.0025 0.0010, -0.0025 0.0005, -0.0030 0.0005, -0.0030 0.0010))', 4326));

-- ============================================================================
-- Ponds - Stock Pond (multipolygon with two ponds)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Ponds";
CREATE TABLE cite."Ponds" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    type TEXT,
    depth_m DOUBLE PRECISION,
    fish_count INTEGER,
    last_stocked TIMESTAMP WITH TIME ZONE,
    geom geometry(MultiPolygon, 4326) NOT NULL
);

INSERT INTO cite."Ponds" (name, description, identifier, type, depth_m, fish_count, last_stocked, geom) VALUES
    ('Stock Pond', 'Fishing pond', 'pond-001', 'Stock Pond', 2.5, 500, '1999-05-01T00:00:00Z', ST_GeomFromText('MULTIPOLYGON(((-0.0020 0.0018, -0.0018 0.0020, -0.0018 0.0016, -0.0020 0.0018)), ((-0.0016 0.0016, -0.0016 0.0020, -0.0014 0.0018, -0.0016 0.0016)))', 4326));

-- ============================================================================
-- RoadSegments - Various road segments (linestrings)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."RoadSegments";
CREATE TABLE cite."RoadSegments" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    num_lanes INTEGER,
    length_km DOUBLE PRECISION,
    last_inspected TIMESTAMP WITH TIME ZONE,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."RoadSegments" (name, description, identifier, num_lanes, length_km, last_inspected, geom) VALUES
    -- First Route 5 segment has NULL num_lanes for PropertyIsNil tests
    ('Route 5', 'Highway segment 1', 'road-001', NULL, 2.5, '1999-12-31T23:00:00Z', ST_GeomFromText('LINESTRING(-0.0042 -0.0006, -0.0032 -0.0003, -0.0026 -0.0001, -0.0014 0.0002, 0.0002 0.0007)', 4326)),
    -- Second Route 5 segment has NULL length for PropertyIsNil tests
    ('Route 5', 'Highway segment 2', 'road-002', 4, NULL, '2000-01-15T10:30:00Z', ST_GeomFromText('LINESTRING(0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014)', 4326)),
    -- Third Route 5 segment has NULL last_inspected for PropertyIsNil tests
    ('Route 5', 'Highway segment 3', 'road-003', 4, 0.9, NULL, ST_GeomFromText('LINESTRING(0.0028 0.0014, 0.0030 0.0024)', 4326)),
    -- Main Street has NULL num_lanes for PropertyIsNil tests
    ('Main Street', 'Downtown main road', 'road-004', NULL, 3.2, '2000-03-10T09:15:00Z', ST_GeomFromText('LINESTRING(0.0002 0.0007, 0.0014 0.0010, 0.0028 0.0014, 0.0042 0.0018)', 4326)),
    -- Dirt Road has NULL length for PropertyIsNil tests
    ('Dirt Road by Green Forest', 'Rural road', 'road-005', 1, NULL, '1999-06-15T16:45:00Z', ST_GeomFromText('LINESTRING(-0.0014 -0.0024, -0.0014 0.0002)', 4326)),
    -- Sixth road has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 1, 0.3, '2000-04-01T12:00:00Z', ST_GeomFromText('LINESTRING(0.0035 0.0000, 0.0040 0.0005)', 4326));

-- ============================================================================
-- Streams - Cam Stream and unnamed stream (linestrings)
-- Added numeric and temporal columns for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Streams";
CREATE TABLE cite."Streams" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    flow_rate DOUBLE PRECISION,
    width_m INTEGER,
    last_measured TIMESTAMP WITH TIME ZONE,
    geom geometry(LineString, 4326) NOT NULL
);

INSERT INTO cite."Streams" (name, description, identifier, flow_rate, width_m, last_measured, geom) VALUES
    -- Cam Stream has NULL width for PropertyIsNil tests
    ('Cam Stream', 'A stream through campus', 'stream-001', 125.5, NULL, '1999-07-01T09:00:00Z', ST_GeomFromText('LINESTRING(-0.0004 0.0024, 0.0002 0.0017, -0.0001 0.0012, 0.0002 0.0007, 0.0010 -0.0006)', 4326)),
    -- Second stream has NULL name for PropertyIsNull tests
    (NULL, NULL, NULL, 45.2, 3, '2000-01-15T14:30:00Z', ST_GeomFromText('LINESTRING(0.0034 -0.0024, 0.0036 -0.0020, 0.0031 -0.0015)', 4326));

-- ============================================================================
-- Autos - Automobiles with time dimension (points)
-- Added 'name' column for PropertyIsLike tests
-- ============================================================================
DROP TABLE IF EXISTS cite."Autos";
CREATE TABLE cite."Autos" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    num INTEGER,
    "time" TIMESTAMP WITH TIME ZONE,
    geom geometry(Point, 4326) NOT NULL
);

INSERT INTO cite."Autos" (name, description, identifier, num, "time", geom) VALUES
    -- Auto1 first row has NULL num for PropertyIsNil tests
    ('Auto1', 'First automobile', 'auto-001', NULL, '2000-01-01T00:00:00Z', ST_SetSRID(ST_MakePoint(-0.0014, 0.0002), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:05Z', ST_SetSRID(ST_MakePoint(-0.0022, 0.0), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:10Z', ST_SetSRID(ST_MakePoint(-0.0029, -0.0002), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:15Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0004), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:20Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0008), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:25Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0014), 4326)),
    ('Auto1', 'First automobile', 'auto-001', 1, '2000-01-01T00:00:30Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0022), 4326)),
    -- Auto2 first row has NULL num for PropertyIsNil tests
    ('Auto2', 'Second automobile', 'auto-002', NULL, '2000-01-01T00:00:20Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0022), 4326)),
    ('Auto2', 'Second automobile', 'auto-002', 2, '2000-01-01T00:00:25Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0012), 4326)),
    ('Auto2', 'Second automobile', 'auto-002', 2, '2000-01-01T00:00:30Z', ST_SetSRID(ST_MakePoint(-0.0032, 0.0002), 4326)),
    ('Auto2', 'Second automobile', 'auto-002', 2, '2000-01-01T00:00:35Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0008), 4326)),
    ('Auto2', 'Second automobile', 'auto-002', 2, '2000-01-01T00:00:40Z', ST_SetSRID(ST_MakePoint(-0.0032, -0.0018), 4326)),
    -- Auto3 first row has NULL num for PropertyIsNil tests
    ('Auto3', 'Third automobile', 'auto-003', NULL, '2000-01-01T00:00:40Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0020), 4326)),
    ('Auto3', 'Third automobile', 'auto-003', 3, '2000-01-01T00:00:45Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0012), 4326)),
    ('Auto3', 'Third automobile', 'auto-003', 3, '2000-01-01T00:00:50Z', ST_SetSRID(ST_MakePoint(-0.0026, -0.0004), 4326)),
    ('Auto3', 'Third automobile', 'auto-003', 3, '2000-01-01T00:00:55Z', ST_SetSRID(ST_MakePoint(-0.0026, 0.0004), 4326)),
    ('Auto3', 'Third automobile', 'auto-003', 3, '2000-01-01T00:01:00Z', ST_SetSRID(ST_MakePoint(-0.0026, 0.0012), 4326)),
    -- Auto4 first row has NULL num for PropertyIsNil tests
    ('Auto4', 'Fourth automobile', 'auto-004', NULL, '2000-01-01T00:00:55Z', ST_SetSRID(ST_MakePoint(0.0029, 0.0019), 4326)),
    ('Auto4', 'Fourth automobile', 'auto-004', 4, '2000-01-01T00:01:00Z', ST_SetSRID(ST_MakePoint(0.0028, 0.0014), 4326)),
    -- NULL name row for PropertyIsNil tests
    (NULL, NULL, NULL, 5, '2000-01-01T00:01:05Z', ST_SetSRID(ST_MakePoint(0.0030, 0.0020), 4326));

-- ============================================================================
-- LakesWithElevation - Lakes with elevation dimension (polygons)
-- Added row with NULL name for PropertyIsNil tests
-- Added temporal column for CITE filter tests
-- ============================================================================
DROP TABLE IF EXISTS cite."LakesWithElevation";
CREATE TABLE cite."LakesWithElevation" (
    id SERIAL PRIMARY KEY,
    name TEXT,
    description TEXT,
    identifier TEXT,
    elev INTEGER,
    depth_m DOUBLE PRECISION,
    surveyed_date TIMESTAMP WITH TIME ZONE,
    geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO cite."LakesWithElevation" (name, description, identifier, elev, depth_m, surveyed_date, geom) VALUES
    -- First Blue Lake has NULL depth for PropertyIsNil tests
    ('Blue Lake', 'A lake at 500m elevation', 'lake-elev-001', 500, NULL, '1998-06-01T08:00:00Z', ST_GeomFromText('POLYGON((0.0006 -0.0018, 0.0010 -0.0006, 0.0024 -0.0001, 0.0031 -0.0015, 0.0006 -0.0018), (0.0017 -0.0011, 0.0025 -0.0011, 0.0025 -0.0006, 0.0017 -0.0006, 0.0017 -0.0011))', 4326)),
    -- Second Blue Lake has NULL elev for PropertyIsNil tests
    ('Blue Lake', 'A lake with unknown elevation', 'lake-elev-002', NULL, 10.2, '1999-03-15T10:30:00Z', ST_GeomFromText('POLYGON((0.0010 -0.0016, 0.0012 -0.0006, 0.0016 -0.0005, 0.0016 -0.0012, 0.0024 -0.0012, 0.0024 -0.0014, 0.0010 -0.0016))', 4326)),
    -- Third Blue Lake has NULL surveyed_date for PropertyIsNil tests
    ('Blue Lake', 'A lake at 480m elevation', 'lake-elev-003', 480, 8.0, NULL, ST_GeomFromText('POLYGON((0.0011 -0.0015, 0.0013 -0.0007, 0.0015 -0.0007, 0.0015 -0.0013, 0.0011 -0.0015))', 4326)),
    -- Fourth row has NULL name for PropertyIsNil tests
    (NULL, NULL, NULL, 470, 5.5, '2000-01-05T16:45:00Z', ST_GeomFromText('POLYGON((0.0012 -0.0014, 0.0014 -0.0008, 0.0014 -0.0010, 0.0012 -0.0014))', 4326));

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
