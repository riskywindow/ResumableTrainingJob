// Binary fake-provisioner runs a dev/test-only ProvisioningRequest backend
// controller that deterministically updates ProvisioningRequest status
// conditions for Phase 7 local development.
//
// This binary is NOT intended for production use. It is deployed as a
// Deployment in the Phase 7 dev profile to exercise the Kueue
// ProvisioningRequest AdmissionCheck flow without a real cluster-autoscaler.
//
// Supported provisioningClassName values:
//
//	check-capacity.fake.dev  — delayed success (default 10s)
//	failed.fake.dev          — permanent failure
//	booking-expiry.fake.dev  — success then capacity revoked
package main

import (
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/example/checkpoint-native-preemption-controller/internal/fakeprovisioner"
)

func main() {
	var metricsAddr string
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9090", "Metrics endpoint bind address.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9091", "Health probe bind address.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := fakeprovisioner.Setup(mgr); err != nil {
		setupLog.Error(err, "unable to setup fake provisioner controller")
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

	setupLog.Info("starting fake provisioner",
		"metricsBindAddress", metricsAddr,
		"healthProbeBindAddress", probeAddr,
		"supportedClasses", []string{
			fakeprovisioner.ClassDelayedSuccess,
			fakeprovisioner.ClassPermanentFailure,
			fakeprovisioner.ClassBookingExpiry,
		},
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
