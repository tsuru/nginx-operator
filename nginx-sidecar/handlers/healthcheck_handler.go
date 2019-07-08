package handlers

import (
	"fmt"
	"net/http"
	"net/url"
)

type Check interface {
	Perform() error
}

type HTTPCheck struct {
	URL string
}

func (c HTTPCheck) Perform() error {
	resp, err := http.Get(c.URL)

	if err != nil || resp.StatusCode >= 400 {
		fmt.Println(err)
		return err
	}

	return nil
}

var checkList []Check

func HealthcheckHandler(w http.ResponseWriter, r *http.Request) {
	urls := r.URL.Query()["url"]

	if len(urls) < 1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if len(checkList) == 0 {
		for _, urlToCheck := range urls {
			urlToCheck, err := url.Parse(urlToCheck)

			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			checkList = append(checkList, HTTPCheck{URL: urlToCheck.String()})
			defer func() { checkList = []Check{} }()
		}
	}

	if err := performChecks(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func performChecks() error {
	for _, check := range checkList {
		if err := check.Perform(); err != nil {
			return err
		}
	}
	return nil
}
