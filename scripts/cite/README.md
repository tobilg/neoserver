# OGC CITE WMS 1.3.0 Test Data Setup

This directory contains scripts to download and set up the OGC CITE (Compliance & Interoperability Testing & Evaluation) test data for WMS 1.3.0 compliance testing.

## Quick Start

Run the complete setup:

```bash
./setup.sh
```

This will:
1. Download the official CITE test data from OGC
2. Extract the data (GML, shapefiles, etc.)
3. Generate a PostGIS SQL script
4. Move the SQL to `testing/initdb/` for Docker initialization

## Individual Scripts

| Script | Description |
|--------|-------------|
| `download.sh` | Downloads the CITE test data ZIP file |
| `extract.sh` | Extracts the ZIP file |
| `generate-sql.sh` | Generates PostGIS SQL from the data |
| `setup.sh` | Runs all steps in sequence |

## Configuration

After running setup, you need to configure the server to scan the `cite` schema:

### Option 1: Config File

In `config/neoserver.toml`:

```toml
[Database]
Schemas = ["public", "cite"]
```

### Option 2: Environment Variable

```bash
export NEOSRV_DATABASE_SCHEMAS="public,cite"
```

## Test Layers

The CITE test data provides the following layers (Blue Lake dataset):

| Layer | Type | Description |
|-------|------|-------------|
| `cite.BasicPolygons` | Polygon | Diamond and two overlapping squares |
| `cite.Lakes` | Polygon | Blue Lake with Goose Island (hole) |
| `cite.Forests` | MultiPolygon | Green Forest area |
| `cite.Bridges` | Point | Cam Bridge |
| `cite.Buildings` | Polygon | Two buildings on Main Street |
| `cite.BuildingCenters` | Point | Building center points |
| `cite.DividedRoutes` | MultiLineString | Route 75 (divided highway) |
| `cite.MapNeatline` | LineString | Map border boundary |
| `cite.NamedPlaces` | Polygon | Ashton and Goose Island |
| `cite.Ponds` | MultiPolygon | Stock Pond (two ponds) |
| `cite.RoadSegments` | LineString | Various road segments |
| `cite.Streams` | LineString | Cam Stream and tributary |
| `cite.Autos` | Point | Automobiles with time dimension |
| `cite.LakesWithElevation` | Polygon | Lakes with elevation dimension |

## Data Source

The test data is downloaded from the official OGC CITE repository:
- URL: https://opengeospatial.github.io/ets-wms13/data-wms-1.3.0.zip
- Documentation: https://cite.opengeospatial.org/teamengine/about/wms13/1.3.0/site/

## Running CITE Tests

After setting up the data, run the CITE test suite:

```bash
# Start the CITE TeamEngine
docker run -p 8081:8080 ogccite/ets-wms13

# Open browser to http://localhost:8081/teamengine/
# Test against: http://host.docker.internal:9000/wms?SERVICE=WMS&REQUEST=GetCapabilities
```

## References

- [OGC WMS 1.3.0 Abstract Test Suite](https://cite.opengeospatial.org/teamengine/about/wms13/1.3.0/site/wms-1_3_0-ats.html)
- [GeoServer CITE Test Guide](https://docs.geoserver.org/latest/en/developer/cite-test-guide/index.html)
- [OGC Simple Features Specification](https://portal.ogc.org/files/?artifact_id=25354)
