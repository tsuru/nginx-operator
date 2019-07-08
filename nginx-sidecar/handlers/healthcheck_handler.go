package handlers

import (
	"log"
	"net/http"
	"net/url"
)

func HealthcheckHandler(w http.ResponseWriter, r *http.Request) {
	urls := r.URL.Query()["url"]

	if len(urls) < 1 {
		log.Println("missing parameter url")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, urlToCheck := range urls {
		urlToCheck, err := url.Parse(urlToCheck)

		if err != nil {
			log.Printf("url format error: %q", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		resp, err := http.Get(urlToCheck.String())

		if err != nil {
			log.Printf("healthcheck request error: %q", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			log.Printf("unexpected status code: %d", resp.StatusCode)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}
