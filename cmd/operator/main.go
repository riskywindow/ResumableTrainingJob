package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	resumeac "github.com/example/checkpoint-native-preemption-controller/internal/admissionchecks/resume"
	"github.com/example/checkpoint-native-preemption-controller/internal/controller"
	kueueintegration "github.com/example/checkpoint-native-preemption-controller/internal/kueue"
	operatormetrics "github.com/example/checkpoint-native-preemption-controller/internal/metrics"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kueuev1beta2.AddToScheme(scheme))
	utilruntime.Must(trainingv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var enableExperimentalPartialAdmission bool
	var modeFlag string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&enableExperimentalPartialAdmission, "enable-experimental-partial-admission", false,
		"Enable the experimental partial-admission path for RTJ. "+
			"When enabled, RTJs with spec.parallelism.enablePartialAdmission=true will "+
			"synthesize PodSet.MinCount for Kueue partial admission. "+
			"Requires Kueue PartialAdmission feature gate (Beta, default-on in v0.15.1).")
	flag.StringVar(&modeFlag, "mode", string(controller.ModeWorker),
		"Operator mode: 'worker' (default) runs the full Phase 5 runtime path "+
			"for single-cluster and MultiKueue worker deployments. "+
			"'manager' suppresses local child JobSet creation for MultiKueue-managed RTJs "+
			"and delegates runtime execution to remote worker clusters.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	operatorMode, err := controller.ParseOperatorMode(modeFlag)
	if err != nil {
		setupLog.Error(err, "invalid --mode flag")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "203ef34d.training.checkpoint.example.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	metricsRecorder := operatormetrics.NewRecorder()

	if err := (&controller.ResumableTrainingJobReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Metrics: metricsRecorder,
		Mode:    operatorMode,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ResumableTrainingJob")
		os.Exit(1)
	}
	if err := (&controller.WorkloadObserver{
		Client:  mgr.GetClient(),
		Metrics: metricsRecorder,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkloadObserver")
		os.Exit(1)
	}

	trainingv1alpha1.SetupResumableTrainingJobWebhookWithManager(mgr)
	trainingv1alpha1.SetupResumeReadinessPolicyWebhookWithManager(mgr)
	trainingv1alpha1.SetupCheckpointPriorityPolicyWebhookWithManager(mgr)

	if err := resumeac.Setup(mgr); err != nil {
		setupLog.Error(err, "unable to setup ResumeReadiness admission check controller")
		os.Exit(1)
	}

	kueueintegration.SetExperimentalPartialAdmission(enableExperimentalPartialAdmission)
	if err := kueueintegration.Setup(context.Background(), mgr); err != nil {
		setupLog.Error(err, "unable to wire RTJ Kueue integration")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info(
		"starting manager",
		"operatorMode", operatorMode,
		"metricsBindAddress", metricsAddr,
		"healthProbeBindAddress", probeAddr,
		"leaderElection", enableLeaderElection,
		"experimentalPartialAdmission", enableExperimentalPartialAdmission,
		"externalFrameworks", kueueintegration.ExternalFrameworks(),
		"phase3Metrics", true,
		"phase4Metrics", true,
		"phase5Metrics", true,
		"phase6OperatorMode", string(operatorMode),
		"resumeReadinessControllerName", resumeac.ControllerName,
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
