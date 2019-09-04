// Copyright 2019 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type remoteServiceSuccess struct{}
type remoteServiceFailure struct{}
type remoteServiceTimeout struct{}

func (f remoteServiceSuccess) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (f remoteServiceFailure) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusBadGateway)
}

func (f remoteServiceTimeout) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	time.Sleep(timeout)
}

type handlerTestCase struct {
	name     string
	query    string
	expected int
}

func TestHealthcheckHandler(t *testing.T) {
	successServer := httptest.NewServer(remoteServiceSuccess{})
	failureServer := httptest.NewServer(remoteServiceFailure{})
	timeoutServer := httptest.NewServer(remoteServiceTimeout{})
	httpsServer := httptest.NewTLSServer(remoteServiceSuccess{})

	testCases := []handlerTestCase{
		{
			name:     "returns-400-when-no-url-param-is-present",
			query:    "",
			expected: http.StatusBadRequest,
		},
		{
			name:     "returns-400-when-url-format-is-invalid",
			query:    "?url=127.0.0.1:8080",
			expected: http.StatusBadRequest,
		},
		{
			name:     "returns-503-when-remote-url-does-not-reply",
			query:    "?url=http://127.0.0.1:81",
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "returns-503-when-external-service-healthcheck-fails",
			query:    "?url=" + failureServer.URL,
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "returns-503-when-external-service-timeouts",
			query:    "?url=" + timeoutServer.URL,
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "returns-200-when-all-checks-succeed",
			query:    "?url=" + successServer.URL,
			expected: http.StatusOK,
		},
		{
			name:     "allows-queries-to-multiple-services",
			query:    "?url=" + successServer.URL + "&url=" + failureServer.URL,
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "works-with-tls-server",
			query:    "?url=" + successServer.URL + "&url=" + httpsServer.URL,
			expected: http.StatusOK,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", tt.query, nil)
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			handler := http.HandlerFunc(HealthcheckHandler)

			handler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expected, recorder.Code)
		})
	}
}
