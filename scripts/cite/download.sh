#!/bin/bash
# Download OGC CITE WMS 1.3.0 test data
# Source: https://opengeospatial.github.io/ets-wms13/

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SCRIPT_DIR}/data"
ZIP_FILE="${DATA_DIR}/data-wms-1.3.0.zip"
DOWNLOAD_URL="https://opengeospatial.github.io/ets-wms13/data-wms-1.3.0.zip"

echo "=== Downloading OGC CITE WMS 1.3.0 Test Data ==="

# Create data directory if it doesn't exist
mkdir -p "${DATA_DIR}"

# Download the data if not already present or force download
if [ -f "${ZIP_FILE}" ] && [ "$1" != "--force" ]; then
    echo "Data already downloaded. Use --force to re-download."
else
    echo "Downloading from ${DOWNLOAD_URL}..."
    curl -L -o "${ZIP_FILE}" "${DOWNLOAD_URL}"
    echo "Download complete: ${ZIP_FILE}"
fi

echo "Done!"
