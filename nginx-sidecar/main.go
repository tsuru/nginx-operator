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
