// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
	"github.com/tsuru/nginx-operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	metricsAddr             = flag.String("metrics-addr", ":8080", "The TCP address that controller should bind to for serving Prometheus metrics. It can be set to \"0\" to disable the metrics serving.")
	healthAddr              = flag.String("health-addr", ":8081", "The TCP address that controller should bind to for serving health probes.")
	enableLeaderElection    = flag.Bool("enable-leader-election", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "", "Namespace where the leader election object will be created.")
	syncPeriod              = flag.Duration("reconcile-sync", 10*time.Hour, "Resync frequency of Nginx resources.")
	logFormat               = flag.String("log-format", "json", "Set the format of logging (options: json, console)")
	logLevel                = zap.LevelFlag("log-level", zapcore.InfoLevel, "Set the level of logging (options: debug, info, warn, error, dpanic, panic, fatal)")
	namespace               = flag.String("namespace", "", "Limit the observed Nginxes to a specific namespace (empty means all namespaces)")
	annotationFilter        = flag.String("annotation-filter", "", "Filter Nginx resources via annotation using label selector semantics (default: all Nginx resources)")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(nginxv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	logEncoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	if *logFormat == "console" {
		logEncoder = zapcore.NewConsoleEncoder(zap.NewProductionEncoderConfig())
	}

	ctrl.SetLogger(ctrlzap.New(
		ctrlzap.Level(zap.NewAtomicLevelAt(*logLevel)),
		ctrlzap.Encoder(logEncoder),
	))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *metricsAddr,
		Namespace:               *namespace,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionID:        "nginx-operator-lock",
		LeaderElectionNamespace: *leaderElectionNamespace,
		SyncPeriod:              syncPeriod,
		HealthProbeBindAddress:  *healthAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// NOTE: registering a dummy checker just to activate the /healthz endpoint
	mgr.AddHealthzCheck("", func(_ *http.Request) error { return nil })

	ls, err := metav1.ParseToLabelSelector(*annotationFilter)
	if err != nil {
		setupLog.Error(err, "unable to convert annotation filter to label selector")
		os.Exit(1)
	}

	annotationSelector, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		setupLog.Error(err, "unable to convert annotation filter to selector")
		os.Exit(1)
	}

	r := &controllers.NginxReconciler{
		Client:           mgr.GetClient(),
		Log:              ctrl.Log.WithName("controllers").WithName("Nginx"),
		Scheme:           mgr.GetScheme(),
		AnnotationFilter: annotationSelector,
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
