package jobset

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// DRAClaimInjection describes a single DRA resource claim to inject into
// rendered child JobSet pod templates. Each injection corresponds to one
// DeviceClaimSpec from the RTJ and its resolved ResourceClaimTemplate name.
type DRAClaimInjection struct {
	// ClaimName is the DeviceClaimSpec.Name (used as PodResourceClaim.Name
	// in the pod spec and as the container Resources.Claims reference).
	ClaimName string

	// TemplateName is the Kubernetes object name of the companion
	// ResourceClaimTemplate (from status.devices.resourceClaimTemplateRefs).
	TemplateName string

	// Containers is the list of container names that should receive
	// this claim (DeviceClaimSpec.Containers). When empty, all containers
	// in the pod receive the claim.
	Containers []string
}

// BuildDRAClaimInjections builds the list of DRA claim injections from
// the RTJ's device spec and status. Returns nil when DRA is not enabled
// or no template refs are available in status yet.
func BuildDRAClaimInjections(rtj *trainingv1alpha1.ResumableTrainingJob) []DRAClaimInjection {
	if !rtj.IsDevicesEnabled() {
		return nil
	}
	if rtj.Status.Devices == nil || len(rtj.Status.Devices.ResourceClaimTemplateRefs) == 0 {
		return nil
	}

	// Build a map from claim name to template name for fast lookup.
	refMap := make(map[string]string, len(rtj.Status.Devices.ResourceClaimTemplateRefs))
	for _, ref := range rtj.Status.Devices.ResourceClaimTemplateRefs {
		refMap[ref.ClaimName] = ref.Name
	}

	var injections []DRAClaimInjection
	for _, claim := range rtj.Spec.Devices.Claims {
		templateName, ok := refMap[claim.Name]
		if !ok {
			continue // Template ref not yet populated for this claim.
		}
		injections = append(injections, DRAClaimInjection{
			ClaimName:    claim.Name,
			TemplateName: templateName,
			Containers:   claim.Containers,
		})
	}
	return injections
}

// InjectDRAClaims adds DRA resource claim references to all replicatedJob
// pod templates in the rendered JobSet spec. For each claim injection:
//
//  1. A PodResourceClaim entry is added to pod.Spec.ResourceClaims[] with
//     ResourceClaimTemplateName pointing at the companion template.
//  2. A ResourceClaim entry is added to container.Resources.Claims[] for
//     each targeted container.
//
// Claims are only injected into replicatedJobs where at least one container
// matches the claim's target container list. This function is idempotent:
// duplicate claim names are skipped. It should be called as the last
// injection step, after topology and podSetUpdate injection.
func InjectDRAClaims(spec *Spec, claims []DRAClaimInjection) {
	if len(claims) == 0 {
		return
	}

	for i := range spec.ReplicatedJobs {
		rj := &spec.ReplicatedJobs[i]
		pod := podSpec(rj)

		for _, claim := range claims {
			// Only inject if at least one container in this pod matches.
			if !anyContainerMatches(pod, claim.Containers) {
				continue
			}

			// Add PodResourceClaim if not already present.
			if !hasPodResourceClaim(pod, claim.ClaimName) {
				pod.ResourceClaims = append(pod.ResourceClaims, corev1.PodResourceClaim{
					Name:                      claim.ClaimName,
					ResourceClaimTemplateName: ptr.To(claim.TemplateName),
				})
			}

			// Attach claim to targeted containers.
			for ci := range pod.Containers {
				if shouldAttachClaim(pod.Containers[ci].Name, claim.Containers) {
					addContainerResourceClaim(&pod.Containers[ci], claim.ClaimName)
				}
			}
		}
	}
}

// anyContainerMatches returns true if at least one container in the pod
// matches the target container list. When targets is empty, all containers
// match (returns true if there are any containers).
func anyContainerMatches(pod *corev1.PodSpec, targets []string) bool {
	if len(targets) == 0 {
		return len(pod.Containers) > 0
	}
	for _, target := range targets {
		for _, c := range pod.Containers {
			if c.Name == target {
				return true
			}
		}
	}
	return false
}

// hasPodResourceClaim returns true if the pod spec already has a
// PodResourceClaim with the given name.
func hasPodResourceClaim(pod *corev1.PodSpec, name string) bool {
	for _, rc := range pod.ResourceClaims {
		if rc.Name == name {
			return true
		}
	}
	return false
}

// shouldAttachClaim returns true if the container should receive the claim.
// When targetContainers is empty, all containers receive the claim.
func shouldAttachClaim(containerName string, targetContainers []string) bool {
	if len(targetContainers) == 0 {
		return true
	}
	for _, name := range targetContainers {
		if name == containerName {
			return true
		}
	}
	return false
}

// addContainerResourceClaim adds a ResourceClaim reference to the container's
// Resources.Claims if not already present.
func addContainerResourceClaim(container *corev1.Container, claimName string) {
	for _, rc := range container.Resources.Claims {
		if rc.Name == claimName {
			return // Already present.
		}
	}
	container.Resources.Claims = append(container.Resources.Claims, corev1.ResourceClaim{
		Name: claimName,
	})
}
