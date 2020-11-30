// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tsuruConfig "github.com/tsuru/config"
	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
	"github.com/tsuru/nginx-operator/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	metricsAddr = flag.String("metrics-addr", ":8080", "The address the metric endpoint binds to.")

	enableLeaderElection    = flag.Bool("enable-leader-election", true, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "", "Namespace where the leader election object will be created.")

	syncPeriod = flag.Duration("reconcile-sync", time.Minute, "Resync frequency of Nginx resources.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(nginxv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.StringVar(&configFile, "c", "", "Path to configuration file")

	flag.Parse()

	if configFile != "" {
		setupLog.Info(fmt.Sprintf("Attempting to load configuration file at %q", configFile))

		if err := tsuruConfig.ReadConfigFile(configFile); err != nil {
			setupLog.Error(err, "Could not read the configuration file")
			os.Exit(1)
		}

		setupLog.Info("Configuration file successfully loaded")
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *metricsAddr,
		Port:                    9443,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionID:        "nginx-operator-lock",
		LeaderElectionNamespace: *leaderElectionNamespace,
		SyncPeriod:              syncPeriod,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	r := &controllers.NginxReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Nginx"),
		Scheme: mgr.GetScheme(),
	}
	if err = r.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Nginx")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
