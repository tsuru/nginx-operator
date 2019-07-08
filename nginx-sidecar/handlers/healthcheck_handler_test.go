package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type remoteServiceSuccess struct{}
type remoteServiceFailure struct{}

func (f remoteServiceSuccess) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (f remoteServiceFailure) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusBadGateway)
}

type handlerTestCase struct {
	name     string
	query    string
	expected int
}

func TestHealthcheckHandler(t *testing.T) {
	successServer := httptest.NewServer(remoteServiceSuccess{})
	failureServer := httptest.NewServer(remoteServiceFailure{})

	testCases := []handlerTestCase{
		{
			name:     "returns-400-when-any-url-param-is-present",
			query:    "",
			expected: http.StatusBadRequest,
		},
		{
			name:     "returns-400-when-url-format-is-invalid",
			query:    "?url=127.0.0.1:8080",
			expected: http.StatusBadRequest,
		},
		{
			name:     "returns-503-when-get-to-remote-url-does-not-reply",
			query:    "?url=http://127.0.0.1:81",
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "returns-503-when-external-service-healthcheck-fails",
			query:    "?url=" + failureServer.URL,
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "returns-200-when-all-checks-success",
			query:    "?url=" + successServer.URL,
			expected: http.StatusOK,
		},
		{
			name:     "allows-queries-to-multiple-services",
			query:    "?url=" + successServer.URL + "&url=" + failureServer.URL,
			expected: http.StatusServiceUnavailable,
		},
	}

	testHandler(t, HealthcheckHandler, testCases)
}

func testHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), testCases []handlerTestCase) {
	for _, testCase := range testCases {
		req, err := http.NewRequest("GET", testCase.query, nil)

		if err != nil {
			t.Fatal(err)
		}

		recorder := httptest.NewRecorder()
		handler := http.HandlerFunc(handler)

		handler.ServeHTTP(recorder, req)

		if status := recorder.Code; status != testCase.expected {
			t.Errorf("%v: expected status code %v got %v", testCase.name, testCase.expected, status)
		}
	}
}
