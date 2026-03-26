package jobset

import (
	"fmt"

	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

// TopologyInjectionResult describes the outcome of topology injection.
type TopologyInjectionResult struct {
	// Injected is true when topology constraints were applied.
	Injected bool

	// NodeSelector contains the topology-derived nodeSelector labels that were
	// injected into the pod template. Nil when not injected.
	NodeSelector map[string]string
}

// InjectTopology applies topology constraints from a parsed topology assignment
// into the pod templates of the rendered JobSet spec. It modifies the spec
// in-place and returns the injection result.
//
// The injection strategy is:
//   - For representable assignments (single-domain or homogeneous higher levels),
//     inject common nodeSelector labels into all containers' pod templates for
//     the specified PodSet.
//   - For non-representable assignments, return an error. The caller is
//     responsible for failing clearly via status conditions.
//
// When topologyResult is nil, this is a no-op (Phase 3 behavior).
func InjectTopology(spec *Spec, workerPodSetName string, topologyResult *topology.ParseResult) (*TopologyInjectionResult, error) {
	if topologyResult == nil {
		return &TopologyInjectionResult{}, nil
	}

	result := &TopologyInjectionResult{}

	for i := range spec.ReplicatedJobs {
		rj := &spec.ReplicatedJobs[i]
		pst, ok := topologyResult.PodSets[rj.Name]
		if !ok || pst == nil {
			continue
		}

		// Check if this assignment can be represented.
		representable, reason := topology.CanRepresentInJobSet(pst)
		if !representable {
			return nil, fmt.Errorf("topology assignment for PodSet %q is not representable in child JobSet: %s", rj.Name, reason)
		}

		// Get the common nodeSelector labels.
		nodeSelector := topology.CommonNodeSelector(pst)
		if len(nodeSelector) == 0 {
			continue
		}

		// Inject into the pod template's nodeSelector.
		pod := podSpec(rj)
		if pod.NodeSelector == nil {
			pod.NodeSelector = make(map[string]string)
		}
		for k, v := range nodeSelector {
			pod.NodeSelector[k] = v
		}

		result.Injected = true
		result.NodeSelector = nodeSelector
	}

	return result, nil
}
