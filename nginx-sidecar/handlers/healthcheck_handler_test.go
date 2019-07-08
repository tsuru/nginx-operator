package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type checkError struct{}
type checkFailureMock struct{}
type checkSuccessMock struct{}

func (c *checkError) Error() string       { return "check error" }
func (m checkFailureMock) Perform() error { return &checkError{} }
func (m checkSuccessMock) Perform() error { return nil }

type handlerTestCase struct {
	name      string
	query     string
	expected  int
	checkList []Check
}

func TestHealthcheckHandler(t *testing.T) {
	testCases := []handlerTestCase{
		{
			name:     "returns-400-when-any-url-param-is-present",
			query:    "",
			expected: http.StatusBadRequest,
		},
		{
			name:      "returns-503-when-a-url-check-fails",
			query:     "?url=http://localhost:8080",
			expected:  http.StatusServiceUnavailable,
			checkList: []Check{checkFailureMock{}},
		},
		{
			name:      "returns-503-when-a-check-fails-after-a-success",
			query:     "?url=http://localhost:8080&url=https://localhost:8443",
			expected:  http.StatusServiceUnavailable,
			checkList: []Check{checkSuccessMock{}, checkFailureMock{}},
		},
		{
			name:      "returns-200-when-all-checks-success",
			query:     "?url=http://localhost:8080&url=https://localhost:8443",
			expected:  http.StatusOK,
			checkList: []Check{checkSuccessMock{}, checkSuccessMock{}},
		},
	}

	testHandler(t, HealthcheckHandler, testCases)
}

func testHandler(t *testing.T, handler func(http.ResponseWriter, *http.Request), testCases []handlerTestCase) {
	for _, testCase := range testCases {
		checkList = testCase.checkList
		defer func() { checkList = []Check{} }()

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
