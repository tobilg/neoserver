#!/bin/bash

# Create a new access token
NEOSRV_STORE_KEY=abc123 $PWD/neoserver create-token --store-path $PWD/data/neoserver.db --role super_admin --expires 24h