#!/usr/bin/env bash

if [ -z "${CLUSTER_NAME}" ]; then
	echo CLUSTER_NAME environment variable is not set
	exit 1
fi
if [ -z "${AWS_ACCOUNT_ID}" ]; then
	echo AWS_ACCOUNT_ID environment variable is not set
	exit 1
fi

eksctl create iamserviceaccount \
  --cluster "${CLUSTER_NAME}" --name node-latency-for-k8s --namespace node-latency-for-k8s \
  --role-name "${CLUSTER_NAME}-node-latency-for-k8s" \
  --attach-policy-arn "arn:aws:iam::${AWS_ACCOUNT_ID}:policy/${CLUSTER_NAME}-node-latency-for-k8s-policy" \
  --role-only \
  --approve
