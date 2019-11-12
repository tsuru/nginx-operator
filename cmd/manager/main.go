// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/leader"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	tsuruConfig "github.com/tsuru/config"
	"github.com/tsuru/nginx-operator/pkg/apis"
	"github.com/tsuru/nginx-operator/pkg/controller"
	nginxOperatorVersion "github.com/tsuru/nginx-operator/version"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

var (
	log = logf.Log.WithName("cmd")

	configFile string
	syncPeriod time.Duration
)

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
	log.Info(fmt.Sprintf("Nginx Operator version: %s)", nginxOperatorVersion.Version))
}

func init() {
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.StringVar(&configFile, "c", "", "Path to configuration file")
	flag.DurationVar(&syncPeriod, "sync-period", time.Duration(5*time.Minute), "Time period which the observed resources are reconciled")
}

func main() {
	flag.Parse()

	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(logf.ZapLogger(false))

	printVersion()

	if configFile != "" {
		log.Info(fmt.Sprintf("Attempting to load configuration file at %q", configFile))

		if err := tsuruConfig.ReadConfigFile(configFile); err != nil {
			log.Error(err, "Could not read the configuration file")
			os.Exit(1)
		}

		log.Info("Configuration file successfully loaded")
	}

	log.Info("Watching all namespaces")

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Become the leader before proceeding
	err = leader.Become(context.TODO(), "nginx-operator-lock")
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{SyncPeriod: &syncPeriod})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
