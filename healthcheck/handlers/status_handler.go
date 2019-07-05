package handlers

import (
	"net/http"
	"strings"
)

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	ports := r.URL.Query()["ports"]

	if len(ports) < 1 {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		ports = strings.Split(ports[0], ",")
		w.WriteHeader(http.StatusOK)
	}
}
