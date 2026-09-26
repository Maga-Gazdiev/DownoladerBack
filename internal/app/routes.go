package app

import "net/http"

func routes(webhook, api http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/webhook", webhook)
	mux.Handle("/api/", api)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}
