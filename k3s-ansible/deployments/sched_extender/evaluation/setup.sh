#!/bin/bash
set -e

# Create results directory
mkdir -p results

# Check if matplotlib and numpy are installed
if ! python3 -c "import matplotlib, numpy" 2>/dev/null; then
  echo "Installing required Python packages..."
  pip install matplotlib numpy
fi

# Check if scheduler extender is deployed
if ! kubectl get pods -n kube-system -l component=scheduler-extender 2>/dev/null | grep Running >/dev/null; then
  echo "Scheduler extender is not running. Do you want to deploy it now? (y/n)"
  read -r answer
  if [[ "$answer" =~ ^[Yy]$ ]]; then
    echo "Deploying scheduler extender..."
    cd ..
    ./kubectl-deploy.sh
    cd evaluation
  else
    echo "Please deploy the scheduler extender before running the evaluation."
    exit 1
  fi
fi

echo "Setup complete. Ready to run evaluation."
echo "Run: python3 run_evaluation.py" 