#!/bin/bash

echo "Provisioning Kind"
kind create cluster --name m3cluster --config kind-cluster.yaml
echo "installing dashboard"
kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.0.0-beta6/aio/deploy/recommended.yaml
echo "creating service account"
kubectl create serviceaccount dashboard -n default
kubectl create clusterrolebinding dashboard-admin -n default --clusterrole=cluster-admin --serviceaccount=default:dashboard

echo "done"

# uncomment this if you want to use the static files
#kubectl apply -f deployments/v3
