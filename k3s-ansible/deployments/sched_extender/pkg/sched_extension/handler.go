package sched_extension

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	schedulerapi "k8s.io/kube-scheduler/extender/v1"
)

// HandleExtenderRequest handles the HTTP request for scheduler extender operations
func HandleExtenderRequest(w http.ResponseWriter, r *http.Request, handler interface{}) {
	log.Printf("DEBUG: Received extender request: %s %s", r.Method, r.URL.Path)

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("DEBUG: Request body length: %d bytes", len(body))

	switch h := handler.(type) {
	case func(schedulerapi.ExtenderArgs) *schedulerapi.ExtenderFilterResult:
		var args schedulerapi.ExtenderArgs
		if err := json.Unmarshal(body, &args); err != nil {
			http.Error(w, fmt.Sprintf("Error parsing request body: %v", err), http.StatusBadRequest)
			return
		}

		// Log filter operation
		log.Printf("=== FILTER OPERATION CALLED ===")
		log.Printf("Pod: %s/%s", args.Pod.Namespace, args.Pod.Name)
		log.Printf("Number of nodes considered: %d", len(args.Nodes.Items))

		result := h(args)

		// Calculate filtered nodes count safely
		nodesRemaining := 0
		if result.Nodes != nil {
			nodesRemaining = len(result.Nodes.Items)
		}
		log.Printf("Filter result: %d nodes filtered, %d nodes remaining",
			len(args.Nodes.Items)-nodesRemaining, nodesRemaining)

		responseBody, err := json.Marshal(result)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(responseBody)

	case func(schedulerapi.ExtenderArgs) *schedulerapi.HostPriorityList:
		var args schedulerapi.ExtenderArgs
		if err := json.Unmarshal(body, &args); err != nil {
			http.Error(w, fmt.Sprintf("Error parsing request body: %v", err), http.StatusBadRequest)
			return
		}

		// Log prioritize operation
		log.Printf("=== PRIORITIZE OPERATION CALLED ===")
		log.Printf("Pod: %s/%s", args.Pod.Namespace, args.Pod.Name)
		log.Printf("Number of nodes to prioritize: %d", len(args.Nodes.Items))

		result := h(args)

		log.Printf("Prioritization complete. Number of scored nodes: %d", len(*result))
		if len(*result) > 0 {
			log.Printf("Top node: %s with score %d", (*result)[0].Host, (*result)[0].Score)
		}

		responseBody, err := json.Marshal(result)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(responseBody)

	default:
		http.Error(w, "Unknown handler type", http.StatusInternalServerError)
	}
}
