package main

import (
	"log"
	"net/http"
	"time"

	"rip-status/web"
)

func main() {
	http.HandleFunc("GET /{$}", web.StatusHandler)
	http.HandleFunc("GET /json", web.JsonHandler)

	srv := &http.Server{
		Addr: ":9511",
		ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe()) 
}