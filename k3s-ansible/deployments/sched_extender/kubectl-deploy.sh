#!/bin/bash
set -e

echo "=== Unified Scheduler Extender Deployment Using Kubectl ==="

# Build and update dependencies
echo "Updating dependencies..."
go mod tidy

# Build and push the Docker image
echo "Building Docker image..."
IMAGE_NAME="cyrusho777/scheduler-extender"
IMAGE_TAG="latest"

# Build the Docker image with proper tags
docker build -t $IMAGE_NAME:$IMAGE_TAG .
echo "Docker image built: $IMAGE_NAME:$IMAGE_TAG"

# Push the image to Docker Hub so Kubernetes can pull it
echo "Pushing image to Docker Hub..."
docker push $IMAGE_NAME:$IMAGE_TAG
echo "Docker image pushed: $IMAGE_NAME:$IMAGE_TAG"

# Apply RBAC configuration
echo "Applying RBAC configuration..."
kubectl apply -f scheduler-extender-rbac.yaml

# Create the service for the scheduler extender
echo "Creating service for the scheduler extender..."
kubectl apply -f scheduler-extender-service.yaml

# Apply the scheduler extender policy ConfigMap
echo "Creating scheduler extender policy..."
kubectl apply -f scheduler-extender-policy.yaml

# Create scheduler configuration
echo "Creating scheduler configuration..."
kubectl apply -f kube-scheduler-config.yaml

# Deploy the scheduler extender
echo "Deploying the scheduler extender..."
kubectl apply -f k8s-deployment.yaml

# Wait for the scheduler extender to be ready
echo "Waiting for the scheduler extender to be ready..."
kubectl wait --for=condition=available --timeout=120s deployment/scheduler-extender -n kube-system

# Patch the k3s scheduler to use our extender - still needs to be done even though we're using kubectl
echo "Patching the k3s scheduler to use our extender..."
kubectl apply -f k3s-scheduler-patch.yaml

# Wait for the k3s scheduler patch to be deployed
echo "Waiting for the k3s scheduler patch to be applied..."
kubectl wait --for=condition=available --timeout=120s deployment/k3s-scheduler-patch -n kube-system

echo "====================================="
echo "Scheduler Extender deployed successfully!"
echo "To verify the deployment, run:"
echo "  kubectl get pods -n kube-system -l component=scheduler-extender"
echo "To test scheduling with the extender, create a pod and observe the logs:"
echo "  kubectl logs -n kube-system -l component=scheduler-extender"
echo "=====================================" 