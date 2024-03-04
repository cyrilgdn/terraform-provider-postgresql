#!/bin/bash

export TF_ACC=true
export PGHOST=<some-flexible-server>.postgres.database.azure.com
export PGPORT=5432
export PGUSER=azure
export PGPASSWORD=$(az account get-access-token --resource-type oss-rdbms --query "[accessToken]" -o tsv)
export PGSSLMODE=require
export PGSUPERUSER=false
export AZURE=true
