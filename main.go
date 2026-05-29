package main

import (
	"log"
	"net/http"
	"time"

	"ripper/internal/preflight"
	"ripper/internal/web"
)

func main() {
	preflight.Init()
	web.OpenStatusFile()

	http.HandleFunc("GET /{$}", web.StatusHandler)
	http.HandleFunc("GET /json", web.JsonHandler)
	http.HandleFunc("GET /logs/{drv}", web.LogHandler)

	srv := &http.Server{
		Addr:         ":9511",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
