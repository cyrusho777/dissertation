#!/usr/bin/env python3
import time
import datetime
import argparse
import json
from kubernetes import client, config
import os

# Configure command-line arguments.
parser = argparse.ArgumentParser(description="Test the preemptive scheduler's ability to handle pod preemption.")
parser.add_argument("--namespace", default="default", help="Kubernetes namespace to use")
parser.add_argument("--poll-interval", type=int, default=2, help="Seconds between polling for pod status")
parser.add_argument("--timeout", type=int, default=300, help="Overall timeout in seconds for the test")
parser.add_argument("--output-file", default="preemption_test_results.json", help="File to save results to")
args = parser.parse_args()

# Load Kubernetes configuration.
try:
    config.load_kube_config()  # for local testing
except:
    config.load_incluster_config()  # when running inside cluster
v1 = client.CoreV1Api()

def create_priority_class(name, value, description):
    """Create a priority class if it doesn't exist."""
    try:
        # Check if priority class exists
        client.SchedulingV1Api().read_priority_class(name=name)
        print(f"Priority class {name} already exists")
    except:
        # Create priority class if it doesn't exist
        priority_class = client.V1PriorityClass(
            metadata=client.V1ObjectMeta(name=name),
            value=value,
            global_default=False,
            description=description
        )
        client.SchedulingV1Api().create_priority_class(body=priority_class)
        print(f"Created priority class: {name} with value {value}")

def setup_priority_classes():
    """Set up the priority classes needed for the test."""
    create_priority_class("low-priority", 10, "Low priority pods")
    create_priority_class("high-priority", 100, "High priority pods")

def submit_pod(pod_name, namespace, image, command, scheduler_name=None, 
               cpu_request="100m", memory_request="128Mi", priority_class=None,
               labels=None):
    """Create a pod with the given specifications."""
    
    # Create pod manifest
    pod_manifest = client.V1Pod(
        metadata=client.V1ObjectMeta(
            name=pod_name,
            labels=labels or {}
        ),
        spec=client.V1PodSpec(
            scheduler_name=scheduler_name,  # Use specified scheduler or default if None
            restart_policy="Never",
            priority_class_name=priority_class,
            tolerations=[
                client.V1Toleration(
                    key="node-role.kubernetes.io/master",
                    operator="Equal",
                    value="true",
                    effect="NoSchedule"
                )
            ],
            containers=[
                client.V1Container(
                    name="container",
                    image=image,
                    command=command,
                    resources=client.V1ResourceRequirements(
                        requests={"cpu": cpu_request, "memory": memory_request},
                        limits={"cpu": cpu_request, "memory": memory_request}
                    )
                )
            ]
        )
    )
    try:
        v1.create_namespaced_pod(namespace=namespace, body=pod_manifest)
        print(f"Created pod: {pod_name}")
        return True
    except Exception as e:
        print(f"Error creating pod {pod_name}: {e}")
        return False

def get_pod_status(pod_name, namespace):
    """Get the current status of a pod."""
    try:
        pod = v1.read_namespaced_pod(name=pod_name, namespace=namespace)
        return {
            "name": pod_name,
            "phase": pod.status.phase,
            "start_time": pod.status.start_time,
            "node_name": pod.spec.node_name if pod.spec.node_name else None
        }
    except Exception as e:
        print(f"Error reading pod {pod_name}: {e}")
        return None

def wait_for_pod_status(pod_name, namespace, desired_status, timeout):
    """Wait for a pod to reach the desired status."""
    print(f"Waiting for pod {pod_name} to reach status: {desired_status}")
    end_time = time.time() + timeout
    while time.time() < end_time:
        status = get_pod_status(pod_name, namespace)
        if status and status["phase"] == desired_status:
            print(f"Pod {pod_name} reached status {desired_status} on node {status['node_name']}")
            return status
        time.sleep(args.poll_interval)
    print(f"Timeout waiting for pod {pod_name} to reach status {desired_status}")
    return None

def cleanup_pods(namespace, label_selector="app=preemption-test"):
    """Delete all test pods."""
    print(f"Cleaning up test pods with label: {label_selector}")
    try:
        v1.delete_collection_namespaced_pod(namespace=namespace, label_selector=label_selector)
        # Wait for pods to be deleted
        time.sleep(10)
        print("Cleanup completed.")
    except Exception as e:
        print(f"Error during cleanup: {e}")

def run_preemption_test():
    """Run a test to verify the preemptive scheduler's ability to handle preemption."""
    results = {
        "test_start_time": datetime.datetime.now().isoformat(),
        "low_priority_pods": [],
        "high_priority_pods": []
    }
    
    try:
        # Set up priority classes
        setup_priority_classes()
        
        # Clean up any existing test pods
        cleanup_pods(args.namespace)
        
        # Step 1: Create low priority pods that consume most of the resources
        print("\n===== STEP 1: Creating low priority pods =====")
        low_priority_pods = []
        for i in range(10):  # Create 10 low priority pods
            pod_name = f"low-priority-pod-{i}"
            labels = {"app": "preemption-test", "priority": "low"}
            
            success = submit_pod(
                pod_name=pod_name,
                namespace=args.namespace,
                image="polinux/stress",
                command=["stress", "--cpu", "1", "--vm", "1", "--vm-bytes", "256M", "--timeout", "300"],
                scheduler_name="preemptive-scheduler",
                cpu_request="500m",
                memory_request="512Mi",
                priority_class="low-priority",
                labels=labels
            )
            
            if success:
                low_priority_pods.append(pod_name)
        
        # Wait for low priority pods to start running
        print("\nWaiting for low priority pods to start running...")
        low_pod_statuses = []
        for pod_name in low_priority_pods:
            status = wait_for_pod_status(pod_name, args.namespace, "Running", 60)
            if status:
                low_pod_statuses.append(status)
        
        print(f"\n{len(low_pod_statuses)} out of {len(low_priority_pods)} low priority pods are running")
        results["low_priority_pods"] = low_pod_statuses
        
        # Step 2: Create high priority pods that should trigger preemption
        print("\n===== STEP 2: Creating high priority pods =====")
        high_priority_pods = []
        for i in range(5):  # Create 5 high priority pods
            pod_name = f"high-priority-pod-{i}"
            labels = {"app": "preemption-test", "priority": "high"}
            
            success = submit_pod(
                pod_name=pod_name,
                namespace=args.namespace,
                image="polinux/stress",
                command=["stress", "--cpu", "1", "--vm", "1", "--vm-bytes", "256M", "--timeout", "300"],
                scheduler_name="preemptive-scheduler",
                cpu_request="1000m",  # Higher resource request to force preemption
                memory_request="1Gi",
                priority_class="high-priority",
                labels=labels
            )
            
            if success:
                high_priority_pods.append(pod_name)
        
        # Wait for high priority pods to start running
        print("\nWaiting for high priority pods to start running...")
        high_pod_statuses = []
        for pod_name in high_priority_pods:
            status = wait_for_pod_status(pod_name, args.namespace, "Running", 120)
            if status:
                high_pod_statuses.append(status)
        
        print(f"\n{len(high_pod_statuses)} out of {len(high_priority_pods)} high priority pods are running")
        results["high_priority_pods"] = high_pod_statuses
        
        # Step 3: Check if any low priority pods were preempted
        print("\n===== STEP 3: Checking for preempted low priority pods =====")
        preempted_pods = []
        for pod_name in low_priority_pods:
            status = get_pod_status(pod_name, args.namespace)
            if status and status["phase"] != "Running":
                print(f"Pod {pod_name} was preempted, current status: {status['phase']}")
                preempted_pods.append(pod_name)
        
        results["preempted_pods"] = preempted_pods
        results["preemption_success"] = len(preempted_pods) > 0
        
        print(f"\nPreemption test results: {len(preempted_pods)} low priority pods were preempted")
        if results["preemption_success"]:
            print("PREEMPTION TEST PASSED: The preemptive scheduler successfully preempted low priority pods")
        else:
            print("PREEMPTION TEST FAILED: No low priority pods were preempted")
        
        # Save results
        with open(args.output_file, 'w') as f:
            # Convert datetime objects to strings for JSON serialization
            for pod_list in [results["low_priority_pods"], results["high_priority_pods"]]:
                for pod in pod_list:
                    if pod and "start_time" in pod and pod["start_time"]:
                        pod["start_time"] = pod["start_time"].isoformat()
            
            json.dump(results, f, indent=2)
            print(f"Results saved to {args.output_file}")
        
        return results
    
    finally:
        # Clean up
        cleanup_pods(args.namespace)

if __name__ == "__main__":
    run_preemption_test() 