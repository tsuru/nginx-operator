// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"net/http"

	"github.com/tsuru/nginx-operator/nginx-sidecar/handlers"
)

const listen = ":59999"

func main() {
	http.HandleFunc("/healthcheck", handlers.HealthcheckHandler)

	log.Fatal(http.ListenAndServe(listen, nil))
}
