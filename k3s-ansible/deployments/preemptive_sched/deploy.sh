#!/bin/bash
set -e

# Configuration
IMAGE_NAME="cyrusho777/preemptive-scheduler"
IMAGE_TAG=${1:-"latest"}
FULL_IMAGE_NAME="${IMAGE_NAME}:${IMAGE_TAG}"

# Ensure dependencies are up to date
echo "Updating dependencies..."
rm -f go.sum
go mod tidy

# Build the Docker image
echo "Building Docker image: ${FULL_IMAGE_NAME}"
docker build -f Dockerfile -t ${FULL_IMAGE_NAME} .

# Push the image to the registry
echo "Pushing image to Docker Hub"
docker push ${FULL_IMAGE_NAME}

# Update the deployment YAML with the correct image
sed -i.bak "s|\${YOUR_REGISTRY}/preemptive-scheduler|${IMAGE_NAME}|g" k8s-deployment.yaml

# Apply Kubernetes resources
echo "Applying Kubernetes resources"
kubectl apply -f k8s-configmap.yaml
kubectl apply -f k8s-deployment.yaml

echo "Deployment complete!"
echo "To check the status, run: kubectl -n kube-system get pods -l app=preemptive-scheduler" 