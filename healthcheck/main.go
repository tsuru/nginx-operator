package main

import (
	"log"
	"net/http"
	"strings"
)

const listen = ":59999"

func statusHandler(w http.ResponseWriter, r *http.Request) {
	ports := r.URL.Query()["ports"]

	if len(ports) < 1 {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		ports = strings.Split(ports[0], ",")
		w.WriteHeader(http.StatusOK)
	}
}

func main() {
	http.HandleFunc("/status", statusHandler)

	log.Fatal(http.ListenAndServe(listen, nil))
}
