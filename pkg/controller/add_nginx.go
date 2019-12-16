// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package controller

import (
	"github.com/tsuru/nginx-operator/pkg/controller/endpoints"
	"github.com/tsuru/nginx-operator/pkg/controller/nginx"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, nginx.Add, endpoints.Add)
}
