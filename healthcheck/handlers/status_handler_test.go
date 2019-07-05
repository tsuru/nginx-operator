package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type handlerTestCase struct {
	name     string
	query    string
	expected int
}

func TestStatusHandler(t *testing.T) {
	testCases := []handlerTestCase{
		{
			name:     "returns-400-when-ports-param-is-not-present",
			query:    "",
			expected: http.StatusBadRequest,
		},
		{
			name:     "returns-200-when-ports-param-is-present",
			query:    "?ports=8080",
			expected: http.StatusOK,
		},
	}

	testHandler(t, StatusHandler, testCases)

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
