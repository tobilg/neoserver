#!/bin/bash
# Complete CITE test data setup script
# Downloads, extracts, and generates SQL for OGC CITE WMS 1.3.0 test data

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=========================================="
echo "OGC CITE WMS 1.3.0 Test Data Setup"
echo "=========================================="
echo ""

# Run all steps
echo "Step 1: Downloading test data..."
"${SCRIPT_DIR}/download.sh"
echo ""

echo "Step 2: Extracting test data..."
"${SCRIPT_DIR}/extract.sh"
echo ""

echo "Step 3: Generating SQL script..."
"${SCRIPT_DIR}/generate-sql.sh"
echo ""

# Move to testing/initdb
INITDB_DIR="${SCRIPT_DIR}/../../testing/initdb"
if [ -d "${INITDB_DIR}" ]; then
    echo "Step 4: Moving SQL to testing/initdb/..."
    mv "${SCRIPT_DIR}/02-cite-data.sql" "${INITDB_DIR}/"
    echo "Moved to: ${INITDB_DIR}/02-cite-data.sql"
fi

echo ""
echo "=========================================="
echo "Setup complete!"
echo "=========================================="
echo ""
echo "Files created:"
echo "  - scripts/cite/data/              - Downloaded and extracted data"
echo "  - scripts/cite/neoserver-cite.toml - CITE test configuration"
echo "  - testing/initdb/02-cite-data.sql - SQL for Docker init"
echo ""
echo "To use CITE test data:"
echo ""
echo "Option 1: Use the provided CITE config file"
echo "  cp scripts/cite/neoserver-cite.toml config/neoserver.toml"
echo "  # Then restart database and server"
echo ""
echo "Option 2: Manually update your existing config"
echo "  1. Add 'cite' to Database.Schemas:"
echo "     Database.Schemas = [\"public\", \"cite\"]"
echo "  2. Add collection overrides to map cite.X to cite:X format"
echo "     (See neoserver-cite.toml for example)"
echo ""
echo "Environment variable alternative:"
echo "  NEOSRV_DATABASE_SCHEMAS=\"public,cite\""
echo ""
echo "After configuration, restart the database and server to load the data."
