#!/bin/bash
# Extract OGC CITE WMS 1.3.0 test data
# Must run download.sh first

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SCRIPT_DIR}/data"
ZIP_FILE="${DATA_DIR}/data-wms-1.3.0.zip"

echo "=== Extracting OGC CITE WMS 1.3.0 Test Data ==="

if [ ! -f "${ZIP_FILE}" ]; then
    echo "Error: ${ZIP_FILE} not found. Run download.sh first."
    exit 1
fi

# Extract to data directory
cd "${DATA_DIR}"
unzip -o "data-wms-1.3.0.zip"

echo ""
echo "Extracted directories:"
ls -la

echo ""
echo "GML files (source data):"
ls -la gml/

echo ""
echo "Shapefile data:"
ls -la shapefile/

echo ""
echo "Done! Data extracted to ${DATA_DIR}"
