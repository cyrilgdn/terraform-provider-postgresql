#!/bin/bash

set -eou pipefail
i=0

while [ $i -lt 10 ]; do
    terraform apply -auto-approve
    terraform destroy -auto-approve
    i=$((i+1))
done