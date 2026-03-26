package resume

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
)

// WorkloadReconciler watches Workload objects and updates their
// AdmissionCheckStates for checks managed by this controller.
//
// For each managed check it:
//  1. Finds the owning RTJ from Workload owner references.
//  2. Loads the ResumeReadinessPolicy from the AdmissionCheck's parameters.
//  3. Queries the checkpoint catalog for a compatible checkpoint (if available).
//  4. Evaluates the readiness decision via the evaluator.
//  5. Maps the decision to an AdmissionCheckState update.
type WorkloadReconciler struct {
	Client client.Client

	// Catalog is the checkpoint catalog used to query available checkpoints.
	// When nil, the evaluator is told no catalog is configured and applies
	// the policy's failurePolicy or allowInitialLaunchWithoutCheckpoint.
	Catalog checkpoints.Catalog

	// ClusterIdentity is the cluster identity string used when building
	// checkpoint resume requests. Defaults to "phase1-kind" if empty.
	ClusterIdentity string
}

// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=training.checkpoint.example.io,resources=resumabletrainingjobs,verbs=get;list;watch

func (r *WorkloadReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx).WithValues("workload", req.NamespacedName)

	wl := &kueuev1beta2.Workload{}
	if err := r.Client.Get(ctx, req.NamespacedName, wl); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Find admission checks managed by this controller.
	managedChecks := r.findManagedChecks(ctx, wl)
	if len(managedChecks) == 0 {
		return reconcile.Result{}, nil
	}

	log.V(1).Info("reconciling Workload for resume-readiness checks", "managedChecks", len(managedChecks))

	// Find the owning RTJ.
	rtj, err := r.findOwningRTJ(ctx, wl)
	if err != nil {
		log.Error(err, "failed to find owning RTJ")
		// Set all managed checks to Retry — we cannot make a decision.
		return r.setAllChecks(ctx, wl, managedChecks, ReadinessDecision{
			State:   kueuev1beta2.CheckStateRetry,
			Reason:  ReasonOwnerNotFound,
			Message: fmt.Sprintf("cannot find owning RTJ: %v", err),
		})
	}
	if rtj == nil {
		// Workload is not owned by an RTJ — mark checks Ready (not our concern).
		return r.setAllChecks(ctx, wl, managedChecks, ReadinessDecision{
			State:   kueuev1beta2.CheckStateReady,
			Reason:  ReasonOwnerNotFound,
			Message: "workload is not owned by a ResumableTrainingJob; defaulting to Ready",
		})
	}

	// Evaluate each managed check independently (each may reference a
	// different AdmissionCheck with a different policy, though in practice
	// there is typically one).
	updated := false
	var requeueAfter time.Duration

	for _, mc := range managedChecks {
		decision := r.evaluateCheck(ctx, mc, rtj)

		if r.applyDecision(wl, mc.checkName, decision) {
			updated = true
		}

		// Retry decisions should cause a requeue.
		if decision.State == kueuev1beta2.CheckStateRetry {
			if requeueAfter == 0 || requeueAfter > 30*time.Second {
				requeueAfter = 30 * time.Second
			}
		}
	}

	if updated {
		if err := r.Client.Status().Update(ctx, wl); err != nil {
			return reconcile.Result{}, fmt.Errorf("update Workload status: %w", err)
		}
		log.Info("updated Workload admission check states", "managedChecks", len(managedChecks))
	}

	if requeueAfter > 0 {
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	}
	return reconcile.Result{}, nil
}

// managedCheck pairs a check name with the AdmissionCheck object.
type managedCheck struct {
	checkName      string
	admissionCheck *kueuev1beta2.AdmissionCheck
}

// findManagedChecks returns admission checks on this workload that are managed
// by this controller.
func (r *WorkloadReconciler) findManagedChecks(ctx context.Context, wl *kueuev1beta2.Workload) []managedCheck {
	var managed []managedCheck
	for _, acs := range wl.Status.AdmissionChecks {
		ac := &kueuev1beta2.AdmissionCheck{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: string(acs.Name)}, ac); err != nil {
			continue
		}
		if ac.Spec.ControllerName == ControllerName {
			managed = append(managed, managedCheck{
				checkName:      string(acs.Name),
				admissionCheck: ac,
			})
		}
	}
	return managed
}

// findOwningRTJ locates the RTJ that owns this Workload via owner references.
func (r *WorkloadReconciler) findOwningRTJ(ctx context.Context, wl *kueuev1beta2.Workload) (*trainingv1alpha1.ResumableTrainingJob, error) {
	for _, ref := range wl.OwnerReferences {
		if ref.APIVersion == trainingv1alpha1.GroupVersion.String() && ref.Kind == "ResumableTrainingJob" {
			rtj := &trainingv1alpha1.ResumableTrainingJob{}
			if err := r.Client.Get(ctx, client.ObjectKey{Namespace: wl.Namespace, Name: ref.Name}, rtj); err != nil {
				return nil, err
			}
			return rtj, nil
		}
	}
	return nil, nil
}

// evaluateCheck runs the evaluator for a single managed check.
func (r *WorkloadReconciler) evaluateCheck(ctx context.Context, mc managedCheck, rtj *trainingv1alpha1.ResumableTrainingJob) ReadinessDecision {
	// Load policy from the AdmissionCheck's parameters.
	policy, err := LoadPolicyForCheck(ctx, r.Client, mc.admissionCheck)
	if err != nil {
		return ReadinessDecision{
			State:   kueuev1beta2.CheckStateRetry,
			Reason:  ReasonPolicyResolutionFailed,
			Message: fmt.Sprintf("cannot load ResumeReadinessPolicy: %v", err),
		}
	}
	resolved := ResolvePolicy(policy)

	// Query the catalog for a compatible checkpoint.
	input := EvaluatorInput{
		RTJ:    rtj,
		Policy: resolved,
		Now:    time.Now(),
	}

	if r.Catalog != nil {
		clusterID := r.ClusterIdentity
		if clusterID == "" {
			clusterID = "phase1-kind"
		}
		resumeReq := checkpoints.ResumeRequestFromRTJ(rtj, clusterID)
		ref, found, catalogErr := r.Catalog.SelectResumeCheckpoint(ctx, resumeReq)
		if catalogErr != nil {
			input.CatalogError = catalogErr
		} else {
			input.CatalogQueried = true
			if found && ref != nil {
				// Convert CheckpointReference back to a manifest for the
				// evaluator. The catalog's SelectResumeCheckpoint already
				// validated completeness and compatibility; we re-wrap it
				// so the evaluator can check age.
				manifest := referenceToManifest(ref)
				input.SelectedCheckpoint = &manifest
			}
		}
	}
	// If Catalog is nil, CatalogQueried stays false and the evaluator
	// handles it via noCatalogDecision.

	return Evaluate(input)
}

// referenceToManifest creates a minimal CheckpointManifest from a
// CheckpointReference so the evaluator can check age. Only fields needed
// by the evaluator (CheckpointID, CompletionTimestamp) are populated.
func referenceToManifest(ref *trainingv1alpha1.CheckpointReference) checkpoints.CheckpointManifest {
	manifest := checkpoints.CheckpointManifest{
		CheckpointID: ref.ID,
		ManifestURI:  ref.ManifestURI,
		WorldSize:    ref.WorldSize,
	}
	if ref.CompletionTime != nil {
		manifest.CompletionTimestamp = ref.CompletionTime.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return manifest
}

// applyDecision updates the AdmissionCheckState for a single check.
// Returns true if the state was modified.
func (r *WorkloadReconciler) applyDecision(wl *kueuev1beta2.Workload, checkName string, decision ReadinessDecision) bool {
	for i := range wl.Status.AdmissionChecks {
		acs := &wl.Status.AdmissionChecks[i]
		if string(acs.Name) != checkName {
			continue
		}

		// No-op if state and message are already the target.
		if acs.State == decision.State && acs.Message == decision.Message {
			return false
		}

		acs.State = decision.State
		acs.Message = decision.Message
		acs.LastTransitionTime = metav1.Now()
		return true
	}
	return false
}

// setAllChecks applies a single decision to all managed checks.
func (r *WorkloadReconciler) setAllChecks(ctx context.Context, wl *kueuev1beta2.Workload, checks []managedCheck, decision ReadinessDecision) (reconcile.Result, error) {
	updated := false
	for _, mc := range checks {
		if r.applyDecision(wl, mc.checkName, decision) {
			updated = true
		}
	}
	if updated {
		if err := r.Client.Status().Update(ctx, wl); err != nil {
			return reconcile.Result{}, fmt.Errorf("update Workload status: %w", err)
		}
	}
	if decision.State == kueuev1beta2.CheckStateRetry {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return reconcile.Result{}, nil
}

// SetupWithManager registers the Workload reconciler with the manager.
func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kueuev1beta2.Workload{}).
		Named("resume-readiness-workload").
		Complete(r)
}
