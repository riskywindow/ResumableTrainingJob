package kueue

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
)

var NewReconciler = jobframework.NewGenericReconcilerFactory(NewGenericJob)

func Setup(ctx context.Context, mgr manager.Manager) error {
	if err := RegisterExternalFramework(); err != nil {
		return fmt.Errorf("register RTJ external framework: %w", err)
	}
	if err := SetupIndexes(ctx, mgr.GetFieldIndexer()); err != nil {
		return fmt.Errorf("setup RTJ workload owner index: %w", err)
	}

	reconciler, err := NewReconciler(
		ctx,
		mgr.GetClient(),
		mgr.GetFieldIndexer(),
		mgr.GetEventRecorderFor("resumabletrainingjob-kueue"),
		jobframework.WithEnabledExternalFrameworks(ExternalFrameworks()),
		jobframework.WithManagerName("checkpoint-native-preemption-controller"),
	)
	if err != nil {
		return fmt.Errorf("build RTJ Kueue reconciler: %w", err)
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup RTJ Kueue reconciler: %w", err)
	}
	return nil
}

func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	return jobframework.SetupWorkloadOwnerIndex(ctx, indexer, GroupVersionKind)
}
