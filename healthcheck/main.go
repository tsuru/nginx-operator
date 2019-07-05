package main

import (
	"github.com/tsuru/nginx-operator/healthcheck/handlers"
	"log"
	"net/http"
)

const listen = ":59999"

func main() {
	http.HandleFunc("/status", handlers.StatusHandler)

	log.Fatal(http.ListenAndServe(listen, nil))
}
