package topology

import (
	"fmt"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// DomainAssignment is a flattened representation of a single topology domain
// with its label key-value pairs and pod count. This is the intermediate form
// used between parsing the Kueue compressed format and injecting constraints
// into child JobSet pod templates.
type DomainAssignment struct {
	// Labels maps topology level keys to their assigned domain values.
	// e.g., {"topology.kubernetes.io/zone": "us-east-1a"}
	Labels map[string]string

	// Count is the number of pods assigned to this domain.
	Count int32
}

// PodSetTopology holds the parsed topology assignment for a single PodSet.
type PodSetTopology struct {
	// PodSetName is the name of the PodSet this assignment applies to.
	PodSetName string

	// Levels are the topology level label keys, ordered from highest to lowest.
	Levels []string

	// Domains are the individual domain assignments with pod counts.
	Domains []DomainAssignment
}

// ParseResult holds the parsed topology for all PodSets in an admission.
type ParseResult struct {
	// PodSets maps PodSet name to its topology assignment.
	PodSets map[string]*PodSetTopology
}

// ParseFromAdmission extracts topology assignments from a Kueue Workload
// Admission. Returns nil when the admission has no topology assignments.
// Returns an error if the compressed slice format cannot be decoded.
func ParseFromAdmission(admission *kueuev1beta2.Admission) (*ParseResult, error) {
	if admission == nil {
		return nil, nil
	}

	result := &ParseResult{
		PodSets: make(map[string]*PodSetTopology),
	}

	hasTopology := false
	for _, psa := range admission.PodSetAssignments {
		if psa.TopologyAssignment == nil {
			continue
		}
		hasTopology = true

		parsed, err := parsePodSetAssignment(string(psa.Name), psa.TopologyAssignment)
		if err != nil {
			return nil, fmt.Errorf("parse topology for PodSet %q: %w", psa.Name, err)
		}
		result.PodSets[string(psa.Name)] = parsed
	}

	if !hasTopology {
		return nil, nil
	}
	return result, nil
}

// parsePodSetAssignment decodes a single PodSet's TopologyAssignment from the
// Kueue compressed slice format into flat DomainAssignment entries.
func parsePodSetAssignment(name string, ta *kueuev1beta2.TopologyAssignment) (*PodSetTopology, error) {
	if len(ta.Levels) == 0 {
		return nil, fmt.Errorf("topology assignment has no levels")
	}

	pst := &PodSetTopology{
		PodSetName: name,
		Levels:     make([]string, len(ta.Levels)),
	}
	copy(pst.Levels, ta.Levels)

	for sliceIdx, slice := range ta.Slices {
		if int(slice.DomainCount) < 1 {
			return nil, fmt.Errorf("slice[%d]: domainCount must be >= 1", sliceIdx)
		}
		if len(slice.ValuesPerLevel) != len(ta.Levels) {
			return nil, fmt.Errorf("slice[%d]: valuesPerLevel length %d does not match levels length %d",
				sliceIdx, len(slice.ValuesPerLevel), len(ta.Levels))
		}

		domains, err := decodeDomains(ta.Levels, &slice)
		if err != nil {
			return nil, fmt.Errorf("slice[%d]: %w", sliceIdx, err)
		}
		pst.Domains = append(pst.Domains, domains...)
	}

	return pst, nil
}

// decodeDomains flattens a single TopologyAssignmentSlice into DomainAssignment
// entries. It handles both Universal and Individual value formats and pod counts.
func decodeDomains(levels []string, slice *kueuev1beta2.TopologyAssignmentSlice) ([]DomainAssignment, error) {
	domainCount := int(slice.DomainCount)

	// Resolve values for each level across all domains.
	levelValues := make([][]string, len(levels))
	for levelIdx, vpl := range slice.ValuesPerLevel {
		values, err := resolveLevelValues(vpl, domainCount)
		if err != nil {
			return nil, fmt.Errorf("level %q: %w", levels[levelIdx], err)
		}
		levelValues[levelIdx] = values
	}

	// Resolve pod counts for each domain.
	podCounts, err := resolvePodCounts(slice.PodCounts, domainCount)
	if err != nil {
		return nil, fmt.Errorf("podCounts: %w", err)
	}

	// Build DomainAssignment for each domain.
	domains := make([]DomainAssignment, domainCount)
	for d := 0; d < domainCount; d++ {
		labels := make(map[string]string, len(levels))
		for l := 0; l < len(levels); l++ {
			labels[levels[l]] = levelValues[l][d]
		}
		domains[d] = DomainAssignment{
			Labels: labels,
			Count:  podCounts[d],
		}
	}
	return domains, nil
}

// resolveLevelValues expands a TopologyAssignmentSliceLevelValues into
// per-domain string values. Handles both Universal and Individual formats.
func resolveLevelValues(vpl kueuev1beta2.TopologyAssignmentSliceLevelValues, domainCount int) ([]string, error) {
	if vpl.Universal != nil {
		// All domains share the same value at this level.
		values := make([]string, domainCount)
		for i := range values {
			values[i] = *vpl.Universal
		}
		return values, nil
	}

	if vpl.Individual != nil {
		if len(vpl.Individual.Roots) != domainCount {
			return nil, fmt.Errorf("individual roots length %d does not match domainCount %d",
				len(vpl.Individual.Roots), domainCount)
		}
		values := make([]string, domainCount)
		prefix := ""
		suffix := ""
		if vpl.Individual.Prefix != nil {
			prefix = *vpl.Individual.Prefix
		}
		if vpl.Individual.Suffix != nil {
			suffix = *vpl.Individual.Suffix
		}
		for i, root := range vpl.Individual.Roots {
			values[i] = prefix + root + suffix
		}
		return values, nil
	}

	return nil, fmt.Errorf("neither universal nor individual values specified")
}

// resolvePodCounts expands TopologyAssignmentSlicePodCounts into per-domain
// pod counts.
func resolvePodCounts(pc kueuev1beta2.TopologyAssignmentSlicePodCounts, domainCount int) ([]int32, error) {
	if pc.Universal != nil {
		counts := make([]int32, domainCount)
		for i := range counts {
			counts[i] = *pc.Universal
		}
		return counts, nil
	}

	if len(pc.Individual) > 0 {
		if len(pc.Individual) != domainCount {
			return nil, fmt.Errorf("individual podCounts length %d does not match domainCount %d",
				len(pc.Individual), domainCount)
		}
		counts := make([]int32, domainCount)
		copy(counts, pc.Individual)
		return counts, nil
	}

	return nil, fmt.Errorf("neither universal nor individual podCounts specified")
}

// ToTopologyStatus converts a PodSetTopology into the RTJ status representation.
func ToTopologyStatus(pst *PodSetTopology) *trainingv1alpha1.TopologyStatus {
	if pst == nil || len(pst.Domains) == 0 {
		return nil
	}

	status := &trainingv1alpha1.TopologyStatus{
		Levels:  make([]string, len(pst.Levels)),
		Domains: make([]trainingv1alpha1.TopologyDomainStatus, len(pst.Domains)),
	}
	copy(status.Levels, pst.Levels)

	for i, domain := range pst.Domains {
		values := make([]string, len(pst.Levels))
		for j, level := range pst.Levels {
			values[j] = domain.Labels[level]
		}
		status.Domains[i] = trainingv1alpha1.TopologyDomainStatus{
			Values: values,
			Count:  domain.Count,
		}
	}

	return status
}

// IsSingleDomain returns true when the assignment has exactly one topology
// domain. Single-domain assignments are trivially representable in a child
// JobSet (one nodeSelector on all pods).
func IsSingleDomain(pst *PodSetTopology) bool {
	return pst != nil && len(pst.Domains) == 1
}

// IsHomogeneous returns true when all domains in the assignment share the same
// label values at every level except the lowest. This is the common case for
// zone-level topology where all pods land in the same zone.
func IsHomogeneous(pst *PodSetTopology) bool {
	if pst == nil || len(pst.Domains) <= 1 {
		return true
	}
	if len(pst.Levels) <= 1 {
		// Single-level topology: domains differ by definition.
		return true
	}
	// Check all levels except the lowest are identical across domains.
	first := pst.Domains[0]
	for _, domain := range pst.Domains[1:] {
		for _, level := range pst.Levels[:len(pst.Levels)-1] {
			if first.Labels[level] != domain.Labels[level] {
				return false
			}
		}
	}
	return true
}

// CanRepresentInJobSet checks whether a topology assignment can be faithfully
// expressed in a child JobSet. Currently, only single-domain assignments or
// multi-domain assignments where all domains share the same top-level values
// can be represented (via a common nodeSelector on the pod template).
//
// Multi-domain assignments that require per-pod scheduling (different nodes in
// different domains) cannot be expressed in a JobSet without scheduling gates
// or per-pod affinity, which are not yet supported.
//
// Returns a human-readable reason when the assignment is not representable.
func CanRepresentInJobSet(pst *PodSetTopology) (bool, string) {
	if pst == nil || len(pst.Domains) == 0 {
		return true, ""
	}

	// Single domain: trivially representable.
	if IsSingleDomain(pst) {
		return true, ""
	}

	// Multi-domain: we can inject a common nodeSelector for levels where all
	// domains share the same value. If the lowest level differs across domains
	// but higher levels are the same, that's representable (common zone selector).
	// If higher levels also differ, we cannot represent this.
	if len(pst.Levels) == 1 {
		// Single-level multi-domain: cannot set a single nodeSelector that
		// matches all domains without an OR. This is not representable.
		return false, fmt.Sprintf(
			"multi-domain assignment across %d domains at level %q cannot be expressed as a single nodeSelector; "+
				"per-pod scheduling gates are required but not yet supported",
			len(pst.Domains), pst.Levels[0])
	}

	// Multi-level: check if higher levels are uniform.
	if !IsHomogeneous(pst) {
		return false, fmt.Sprintf(
			"multi-domain assignment spans different values at higher topology levels %v; "+
				"only assignments where higher levels are uniform can be expressed in a child JobSet",
			pst.Levels[:len(pst.Levels)-1])
	}

	// Higher levels are uniform: we can set a nodeSelector for the uniform levels.
	return true, ""
}

// CommonNodeSelector returns the nodeSelector labels that are common across all
// domains in the assignment. These can safely be injected into the pod template
// of the child JobSet. Returns nil if there are no common labels.
func CommonNodeSelector(pst *PodSetTopology) map[string]string {
	if pst == nil || len(pst.Domains) == 0 {
		return nil
	}

	if IsSingleDomain(pst) {
		// All labels are common for a single domain.
		result := make(map[string]string, len(pst.Levels))
		for level, value := range pst.Domains[0].Labels {
			result[level] = value
		}
		return result
	}

	// Multi-domain: find labels that are the same across all domains.
	result := make(map[string]string)
	for _, level := range pst.Levels {
		firstVal := pst.Domains[0].Labels[level]
		allSame := true
		for _, domain := range pst.Domains[1:] {
			if domain.Labels[level] != firstVal {
				allSame = false
				break
			}
		}
		if allSame {
			result[level] = firstVal
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
