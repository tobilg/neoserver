CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_raster;

DROP TABLE IF EXISTS public.places;
CREATE TABLE public.places (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  identifier TEXT,
  category TEXT,
  geom geometry(Point, 4326) NOT NULL
);

INSERT INTO public.places (name, description, identifier, category, geom) VALUES
  ('Berlin', 'Capital of Germany', 'place-berlin', 'city', ST_SetSRID(ST_MakePoint(13.4050, 52.5200), 4326)),
  ('Munich', 'Capital of Bavaria', 'place-munich', 'city', ST_SetSRID(ST_MakePoint(11.5820, 48.1351), 4326)),
  ('Hamburg', 'City on the Elbe', 'place-hamburg', 'city', ST_SetSRID(ST_MakePoint(9.9937, 53.5511), 4326));

-- A deterministic 10x10 regular grid used by WCS backend/integration tests.
DROP TABLE IF EXISTS public.wcs_fixture;
CREATE TABLE public.wcs_fixture (id SERIAL PRIMARY KEY, rast raster NOT NULL);
INSERT INTO public.wcs_fixture (rast)
SELECT ST_AddBand(
  ST_AddBand(
    ST_SetValues(
      ST_AddBand(ST_MakeEmptyRaster(10, 10, 10.0, 55.0, 0.1, -0.1, 0, 0, 4326), '32BF'::text, 0, -9999),
      1, 1, 1,
      ARRAY[
        ARRAY[0,1,2,3,4,5,6,7,8,9]::double precision[],
        ARRAY[10,11,12,13,14,15,16,17,18,19]::double precision[],
        ARRAY[20,21,22,23,24,25,26,27,28,29]::double precision[],
        ARRAY[30,31,32,33,34,35,36,37,38,39]::double precision[],
        ARRAY[40,41,42,43,44,45,46,47,48,49]::double precision[],
        ARRAY[50,51,52,53,54,55,56,57,58,59]::double precision[],
        ARRAY[60,61,62,63,64,65,66,67,68,69]::double precision[],
        ARRAY[70,71,72,73,74,75,76,77,78,79]::double precision[],
        ARRAY[80,81,82,83,84,85,86,87,88,89]::double precision[],
        ARRAY[90,91,92,93,94,95,96,97,98,99]::double precision[]
      ]::double precision[][]
    ),
    '32BF'::text, 100, -9999
  ),
  '32BF'::text, 200, -9999
);
CREATE INDEX wcs_fixture_rast_idx ON public.wcs_fixture USING GIST (ST_ConvexHull(rast));
-- Cast all identifiers to name so PostgreSQL selects the schema-qualified
-- overload rather than the two-name-plus-variadic-text overload.
SELECT AddRasterConstraints('public'::name, 'wcs_fixture'::name, 'rast'::name);

-- A second independent coverage is required by the official WCS ETS. Keeping
-- its grid deterministic but spatially separate also catches accidental
-- cross-coverage selection in DescribeCoverage/GetCoverage requests.
DROP TABLE IF EXISTS public.wcs_fixture_aux;
CREATE TABLE public.wcs_fixture_aux (id SERIAL PRIMARY KEY, rast raster NOT NULL);
INSERT INTO public.wcs_fixture_aux (rast)
SELECT ST_SetUpperLeft(ST_Band(rast, 1), 12.0, 55.0) FROM public.wcs_fixture;
CREATE INDEX wcs_fixture_aux_rast_idx ON public.wcs_fixture_aux USING GIST (ST_ConvexHull(rast));
SELECT AddRasterConstraints('public'::name, 'wcs_fixture_aux'::name, 'rast'::name);

DROP TABLE IF EXISTS public.areas;
CREATE TABLE public.areas (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  identifier TEXT,
  category TEXT,
  geom geometry(Polygon, 4326) NOT NULL
);

INSERT INTO public.areas (name, description, identifier, category, geom) VALUES
  ('SampleSquare', 'Sample square area', 'area-sample', 'sample', ST_GeomFromText('POLYGON((13.3 52.45, 13.5 52.45, 13.5 52.6, 13.3 52.6, 13.3 52.45))', 4326)),
  ('BerlinArea', 'Berlin metropolitan area', 'area-berlin', 'city', ST_GeomFromText('POLYGON((13.0 52.3, 13.8 52.3, 13.8 52.7, 13.0 52.7, 13.0 52.3))', 4326)),
  ('MunichArea', 'Munich metropolitan area', 'area-munich', 'city', ST_GeomFromText('POLYGON((11.3 47.9, 11.9 47.9, 11.9 48.3, 11.3 48.3, 11.3 47.9))', 4326)),
  ('HamburgArea', 'Hamburg metropolitan area', 'area-hamburg', 'city', ST_GeomFromText('POLYGON((9.7 53.4, 10.3 53.4, 10.3 53.7, 9.7 53.7, 9.7 53.4))', 4326)),
  ('FrankfurtArea', 'Frankfurt metropolitan area', 'area-frankfurt', 'city', ST_GeomFromText('POLYGON((8.4 50.0, 9.0 50.0, 9.0 50.3, 8.4 50.3, 8.4 50.0))', 4326));
