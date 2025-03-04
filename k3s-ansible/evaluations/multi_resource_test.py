#!/usr/bin/env python3

import argparse
import json
import os
import subprocess
import time
from datetime import datetime
from kubernetes import client, config

def parse_args():
    parser = argparse.ArgumentParser(description='Test multi-resource scheduler pod scheduling')
    parser.add_argument('--namespace', default='default', help='Namespace to run the test in')
    parser.add_argument('--num-pods', type=int, default=5, help='Number of pods to create')
    parser.add_argument('--cpu-request', default='100m', help='CPU request for each pod')
    parser.add_argument('--memory-request', default='128Mi', help='Memory request for each pod')
    parser.add_argument('--poll-interval', type=int, default=2, help='Interval in seconds to poll pod status')
    parser.add_argument('--timeout', type=int, default=300, help='Timeout in seconds for the test')
    parser.add_argument('--output-file', default='multi_resource_test_results.json', help='File to write test results to')
    return parser.parse_args()

def create_pod_manifest(name, namespace, cpu_request, memory_request):
    return {
        'apiVersion': 'v1',
        'kind': 'Pod',
        'metadata': {
            'name': name,
            'namespace': namespace,
            'labels': {
                'app': 'multi-resource-test'
            }
        },
        'spec': {
            'schedulerName': 'multi-resource-scheduler',
            'containers': [{
                'name': 'pause',
                'image': 'k8s.gcr.io/pause:3.2',
                'resources': {
                    'requests': {
                        'cpu': cpu_request,
                        'memory': memory_request
                    },
                    'limits': {
                        'cpu': cpu_request,
                        'memory': memory_request
                    }
                }
            }],
            'terminationGracePeriodSeconds': 5
        }
    }

def submit_pod(manifest):
    api = client.CoreV1Api()
    return api.create_namespaced_pod(
        body=manifest,
        namespace=manifest['metadata']['namespace']
    )

def check_pod_status(name, namespace):
    api = client.CoreV1Api()
    pod = api.read_namespaced_pod(name=name, namespace=namespace)
    return pod.status.phase, pod.status.conditions

def run_multi_resource_test(args):
    # Initialize Kubernetes client
    try:
        config.load_kube_config()
    except:
        config.load_incluster_config()
    
    api = client.CoreV1Api()
    
    # Create test pods
    print(f"Creating {args.num_pods} test pods...")
    pods = []
    for i in range(args.num_pods):
        pod_name = f"multi-resource-pod-{i}"
        pod_manifest = create_pod_manifest(
            pod_name, 
            args.namespace, 
            args.cpu_request, 
            args.memory_request
        )
        try:
            pod = submit_pod(pod_manifest)
            pods.append(pod.metadata.name)
            print(f"Created pod {pod.metadata.name}")
        except Exception as e:
            print(f"Error creating pod {pod_name}: {e}")
    
    # Monitor pod status
    start_time = time.time()
    scheduled_pods = []
    unscheduled_pods = []
    
    print("Monitoring pod status...")
    while time.time() - start_time < args.timeout:
        all_scheduled = True
        for pod_name in pods:
            try:
                phase, conditions = check_pod_status(pod_name, args.namespace)
                
                # Check if pod is scheduled
                scheduled = False
                for condition in conditions:
                    if condition.type == 'PodScheduled' and condition.status == 'True':
                        scheduled = True
                        if pod_name not in scheduled_pods:
                            scheduled_pods.append(pod_name)
                            print(f"Pod {pod_name} has been scheduled")
                        break
                
                if not scheduled:
                    all_scheduled = False
                    if pod_name not in unscheduled_pods:
                        unscheduled_pods.append(pod_name)
            except Exception as e:
                print(f"Error checking pod {pod_name} status: {e}")
                all_scheduled = False
        
        if all_scheduled and len(scheduled_pods) == args.num_pods:
            print("All pods have been scheduled!")
            break
        
        time.sleep(args.poll_interval)
    
    # Collect results
    end_time = time.time()
    test_duration = end_time - start_time
    
    # Get final pod status
    final_pod_status = {}
    for pod_name in pods:
        try:
            pod = api.read_namespaced_pod(name=pod_name, namespace=args.namespace)
            final_pod_status[pod_name] = {
                'phase': pod.status.phase,
                'node': pod.spec.node_name if pod.spec.node_name else None,
                'conditions': [{'type': c.type, 'status': c.status, 'reason': c.reason} for c in pod.status.conditions]
            }
        except Exception as e:
            final_pod_status[pod_name] = {'error': str(e)}
    
    # Get node resource usage
    nodes = api.list_node().items
    node_status = {}
    for node in nodes:
        node_name = node.metadata.name
        node_status[node_name] = {
            'capacity': {
                'cpu': node.status.capacity.get('cpu'),
                'memory': node.status.capacity.get('memory')
            },
            'allocatable': {
                'cpu': node.status.allocatable.get('cpu'),
                'memory': node.status.allocatable.get('memory')
            }
        }
    
    # Prepare results
    results = {
        'test_start': datetime.fromtimestamp(start_time).isoformat(),
        'test_end': datetime.fromtimestamp(end_time).isoformat(),
        'test_duration_seconds': test_duration,
        'num_pods': args.num_pods,
        'cpu_request': args.cpu_request,
        'memory_request': args.memory_request,
        'scheduled_pods': scheduled_pods,
        'unscheduled_pods': unscheduled_pods,
        'pod_status': final_pod_status,
        'node_status': node_status
    }
    
    # Write results to file
    with open(args.output_file, 'w') as f:
        json.dump(results, f, indent=2)
    
    print(f"Test completed in {test_duration:.2f} seconds")
    print(f"Scheduled pods: {len(scheduled_pods)}/{args.num_pods}")
    print(f"Results written to {args.output_file}")
    
    # Clean up
    print("Cleaning up test pods...")
    for pod_name in pods:
        try:
            api.delete_namespaced_pod(name=pod_name, namespace=args.namespace)
            print(f"Deleted pod {pod_name}")
        except Exception as e:
            print(f"Error deleting pod {pod_name}: {e}")
    
    return results

if __name__ == '__main__':
    args = parse_args()
    run_multi_resource_test(args) 