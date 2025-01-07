// Copyright 2020 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	nginxv1alpha1 "github.com/tsuru/nginx-operator/api/v1alpha1"
	"github.com/tsuru/nginx-operator/controllers"
	"github.com/tsuru/nginx-operator/pkg/gcp"
	"github.com/tsuru/nginx-operator/version"

	// +kubebuilder:scaffold:imports

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	scheme = runtime.NewScheme()

	// Following the standard of flags on Kubernetes.
	// See more: https://github.com/kubernetes-sigs/kubebuilder/issues/1839
	metricsAddr = flag.String("metrics-bind-address", ":8080", "The TCP address that controller should bind to for serving Prometheus metrics. It can be set to \"0\" to disable the metrics serving.")
	healthAddr  = flag.String("health-probe-bind-address", ":8081", "The TCP address that controller should bind to for serving health probes.")

	leaderElection                  = flag.Bool("leader-elect", true, "Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability.")
	leaderElectionResourceName      = flag.String("leader-elect-resource-name", "nginx-operator-lock", "The name of resource object that is used for locking during leader election.")
	leaderElectionResourceNamespace = flag.String("leader-elect-resource-namespace", "", "The namespace of resource object that is used for locking during leader election.")

	syncPeriod = flag.Duration("sync-period", 10*time.Hour, "The resync period for reconciling manager resources.")

	logFormat = flag.String("log-format", "json", "Set the format of logging (options: json, console)")
	logLevel  = zap.LevelFlag("log-level", zapcore.InfoLevel, "Set the level of logging (options: debug, info, warn, error, dpanic, panic, fatal)")

	namespace        = flag.String("namespace", "", "Limit the observed Nginx resources from specific namespace (empty means all namespaces)")
	annotationFilter = flag.String("annotation-filter", "", "Filter Nginx resources via annotation using label selector semantics (default: all Nginx resources)")
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
		Scheme:                     scheme,
		MetricsBindAddress:         *metricsAddr,
		Namespace:                  *namespace,
		LeaderElectionResourceLock: "leases",
		LeaderElection:             *leaderElection,
		LeaderElectionID:           *leaderElectionResourceName,
		LeaderElectionNamespace:    *leaderElectionResourceNamespace,
		SyncPeriod:                 syncPeriod,
		HealthProbeBindAddress:     *healthAddr,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ls, err := metav1.ParseToLabelSelector(*annotationFilter)
	if err != nil {
		ctrl.Log.Error(err, "unable to convert annotation filter to label selector")
		os.Exit(1)
	}

	annotationSelector, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		ctrl.Log.Error(err, "unable to convert annotation filter to selector")
		os.Exit(1)
	}

	err = (&controllers.NginxReconciler{
		Client:           mgr.GetClient(),
		EventRecorder:    mgr.GetEventRecorderFor("nginx-operator"),
		Log:              ctrl.Log.WithName("controllers").WithName("Nginx"),
		Scheme:           mgr.GetScheme(),
		AnnotationFilter: annotationSelector,
		GcpClient:        gcp.NewGcpClient(os.Getenv("GCP_PROJECT_ID")),
	}).SetupWithManager(mgr)
	if err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "Nginx")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.Info("starting manager", "version", version.Version, "commit", version.GitCommit)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "problem running manager")
		os.Exit(1)
	}
}
