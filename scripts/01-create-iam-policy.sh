#!/usr/bin/env bash

if [ -z "${CLUSTER_NAME}" ]; then
	echo CLUSTER_NAME environment variable is not set
	exit 1
fi

aws cloudformation deploy \
  --stack-name "${CLUSTER_NAME}-node-latency-for-k8s" \
  --template-file cloudformation.yaml \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides "ClusterName=${CLUSTER_NAME}"
