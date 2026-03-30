// Package remote provides helpers for resolving multi-cluster execution
// context when the operator runs in manager mode with MultiKueue dispatch.
package remote

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// MultiKueueAdmissionCheckController is the controller name that Kueue uses
// for MultiKueue admission checks. Duplicated here to avoid a dependency
// cycle with internal/multikueue.
const MultiKueueAdmissionCheckController = "kueue.x-k8s.io/multikueue"

// ClusterResolver extracts the execution cluster name for a MultiKueue-
// dispatched RTJ. The execution cluster is the worker cluster where the
// remote RTJ copy is running.
type ClusterResolver interface {
	// ResolveExecutionCluster returns the worker cluster name for the given
	// RTJ. Returns ("", nil) when the cluster cannot be determined yet
	// (e.g. the Workload has not been dispatched).
	ResolveExecutionCluster(ctx context.Context, job *trainingv1alpha1.ResumableTrainingJob) (string, error)
}

// WorkloadClusterResolver resolves the execution cluster by inspecting the
// Kueue Workload's admission check status. When MultiKueue dispatches a
// Workload to a worker cluster, it sets the MultiKueue admission check
// state to Ready and stores the worker cluster name in the admission
// check's Message field.
type WorkloadClusterResolver struct {
	Client client.Client

	// AdmissionCheckName is the name of the MultiKueue admission check
	// resource on the manager cluster. When empty, all admission checks
	// with the MultiKueue controller name are searched.
	AdmissionCheckName string
}

// ResolveExecutionCluster reads the Workload referenced by the RTJ and
// extracts the worker cluster name from the MultiKueue admission check.
func (r *WorkloadClusterResolver) ResolveExecutionCluster(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
) (string, error) {
	ref := job.Status.WorkloadReference
	if ref == nil || ref.Name == "" {
		return "", nil
	}

	ns := ref.Namespace
	if ns == "" {
		ns = job.Namespace
	}

	var wl kueuev1beta2.Workload
	key := types.NamespacedName{Name: ref.Name, Namespace: ns}
	if err := r.Client.Get(ctx, key, &wl); err != nil {
		return "", fmt.Errorf("get workload %s: %w", key, err)
	}

	return extractClusterFromAdmissionChecks(wl.Status.AdmissionChecks, r.AdmissionCheckName), nil
}

// extractClusterFromAdmissionChecks scans the admission check list for a
// MultiKueue check in Ready state and returns the cluster name from its
// Message field. Returns "" when no match is found.
func extractClusterFromAdmissionChecks(
	checks []kueuev1beta2.AdmissionCheckState,
	admissionCheckName string,
) string {
	for _, check := range checks {
		if admissionCheckName != "" && string(check.Name) != admissionCheckName {
			continue
		}
		if check.State == kueuev1beta2.CheckStateReady && check.Message != "" {
			return check.Message
		}
	}
	return ""
}

// StaticClusterResolver always returns a fixed cluster name. Useful for
// testing and for environments where the execution cluster is known
// ahead of time.
type StaticClusterResolver struct {
	ClusterName string
}

// ResolveExecutionCluster returns the static cluster name.
func (r *StaticClusterResolver) ResolveExecutionCluster(
	_ context.Context,
	_ *trainingv1alpha1.ResumableTrainingJob,
) (string, error) {
	return r.ClusterName, nil
}
