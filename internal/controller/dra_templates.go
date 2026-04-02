package controller

import (
	"context"
	"fmt"

	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/dra"
)

// DRATemplateReconcileResult captures the outcome of DRA template
// reconciliation, including whether status fields need updating.
type DRATemplateReconcileResult struct {
	// StatusChanged is true when any status.devices field was modified.
	StatusChanged bool

	// TemplatesReady is true when all desired templates exist and match
	// the spec. False when templates were created, updated, or are
	// pending reconciliation.
	TemplatesReady bool
}

// reconcileDRATemplates ensures that the set of ResourceClaimTemplate
// objects matches the RTJ's spec.devices declaration. It is:
//
//   - Idempotent: re-running produces the same result.
//   - Owner-reference aware: templates are owned by the RTJ.
//   - Safe across spec changes: detects spec drift and recreates.
//   - Safe across operator restarts: converges from any state.
//
// This function does NOT render DRA fields into the child JobSet.
// It only manages the companion ResourceClaimTemplate objects and
// syncs status.devices.
//
// Returns a result indicating whether status changed and whether
// templates are ready.
func (r *ResumableTrainingJobReconciler) reconcileDRATemplates(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	now metav1.Time,
) (*DRATemplateReconcileResult, error) {
	logger := log.FromContext(ctx)
	result := &DRATemplateReconcileResult{}

	// When devices are not configured, clear device status and return.
	if !job.IsDevicesEnabled() {
		result.StatusChanged = clearDeviceStatus(job)
		result.TemplatesReady = true
		return result, nil
	}

	// Build the device profile fingerprint.
	profile := dra.BuildProfile(job.Spec.Devices)

	// Build the desired set of templates.
	desired := dra.BuildDesiredTemplates(job)
	if len(desired) == 0 {
		result.StatusChanged = clearDeviceStatus(job)
		result.TemplatesReady = true
		return result, nil
	}

	// Reconcile each desired template.
	allReady := true
	for _, dt := range desired {
		ready, err := r.reconcileSingleTemplate(ctx, job, dt)
		if err != nil {
			return nil, fmt.Errorf("reconcile template %s: %w", dt.Name, err)
		}
		if !ready {
			allReady = false
		}
	}

	// Clean up orphaned templates (templates owned by this RTJ that are
	// not in the desired set).
	if err := r.cleanupOrphanedTemplates(ctx, job, desired); err != nil {
		return nil, fmt.Errorf("cleanup orphaned templates: %w", err)
	}

	// Sync device status.
	refs := dra.TemplateRefs(job.Name, job.Spec.Devices.Claims)
	result.StatusChanged = syncDeviceStatus(job, profile, refs)
	result.TemplatesReady = allReady

	logger.Info("DRA templates reconciled",
		"templateCount", len(desired),
		"allReady", allReady,
		"fingerprint", profile.Fingerprint,
	)

	return result, nil
}

// reconcileSingleTemplate ensures a single ResourceClaimTemplate exists
// and matches the desired spec. Returns true when the template is ready
// (exists and matches).
func (r *ResumableTrainingJobReconciler) reconcileSingleTemplate(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	dt dra.DesiredTemplate,
) (bool, error) {
	logger := log.FromContext(ctx)
	key := dra.TemplateKey(job.Namespace, dt.Name)

	existing := &resourcev1beta1.ResourceClaimTemplate{}
	err := r.Get(ctx, key, existing)

	if apierrors.IsNotFound(err) {
		// Template does not exist — create it.
		logger.Info("creating ResourceClaimTemplate",
			"name", dt.Name,
			"claimName", dt.ClaimName,
		)
		if err := r.Create(ctx, dt.Template); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Lost a race, will converge on next reconcile.
				return false, nil
			}
			return false, fmt.Errorf("create ResourceClaimTemplate %s: %w", dt.Name, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get ResourceClaimTemplate %s: %w", dt.Name, err)
	}

	// Template exists — check if the spec matches.
	if dra.TemplateSpecMatches(existing, dt.Template) {
		// Spec matches, template is ready.
		return true, nil
	}

	// Spec drift detected — delete and recreate.
	// ResourceClaimTemplate spec fields (deviceClassName, count, selectors)
	// are immutable in practice (the kubelet creates ResourceClaims from
	// the template, and changing the template under active claims would
	// be unsafe). Delete + recreate is the safe path.
	logger.Info("spec drift detected, recreating ResourceClaimTemplate",
		"name", dt.Name,
		"claimName", dt.ClaimName,
	)

	if err := r.Delete(ctx, existing); err != nil {
		if apierrors.IsNotFound(err) {
			// Already gone, proceed with create.
		} else {
			return false, fmt.Errorf("delete drifted ResourceClaimTemplate %s: %w", dt.Name, err)
		}
	}

	if err := r.Create(ctx, dt.Template); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, fmt.Errorf("recreate ResourceClaimTemplate %s: %w", dt.Name, err)
	}

	return true, nil
}

// cleanupOrphanedTemplates deletes ResourceClaimTemplates owned by this
// RTJ that are not in the desired set. This handles the case where a
// claim is removed from spec.devices.claims.
func (r *ResumableTrainingJobReconciler) cleanupOrphanedTemplates(
	ctx context.Context,
	job *trainingv1alpha1.ResumableTrainingJob,
	desired []dra.DesiredTemplate,
) error {
	logger := log.FromContext(ctx)

	// Build the set of desired template names.
	desiredNames := make(map[string]bool, len(desired))
	for _, dt := range desired {
		desiredNames[dt.Name] = true
	}

	// List all ResourceClaimTemplates in the RTJ's namespace with the
	// RTJ-name label.
	var templateList resourcev1beta1.ResourceClaimTemplateList
	if err := r.List(ctx, &templateList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{
			"training.checkpoint.example.io/rtj-name":   job.Name,
			"training.checkpoint.example.io/managed-by": "rtj-operator",
		},
	); err != nil {
		return fmt.Errorf("list ResourceClaimTemplates: %w", err)
	}

	for i := range templateList.Items {
		tmpl := &templateList.Items[i]

		// Skip templates that are in the desired set.
		if desiredNames[tmpl.Name] {
			continue
		}

		// Verify this template is actually owned by this RTJ (defensive).
		if !isOwnedByRTJ(tmpl.OwnerReferences, job) {
			continue
		}

		logger.Info("deleting orphaned ResourceClaimTemplate",
			"name", tmpl.Name,
		)
		if err := r.Delete(ctx, tmpl); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("delete orphaned template %s: %w", tmpl.Name, err)
		}
	}

	return nil
}

// isOwnedByRTJ checks if the owner references include this RTJ as
// the controller owner.
func isOwnedByRTJ(refs []metav1.OwnerReference, job *trainingv1alpha1.ResumableTrainingJob) bool {
	for _, ref := range refs {
		if ref.Kind == dra.RTJGroupVersionKind.Kind &&
			ref.Name == job.Name &&
			ref.UID == job.UID {
			return true
		}
	}
	return false
}
