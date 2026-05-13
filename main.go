package main

import (
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"github.com/joho/godotenv"
)

var (
	statusRoot *os.Root
	statusFile string
)

func gatherStatus() (string, error) {
	file, err := statusRoot.Open(statusFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func statusHandler (w http.ResponseWriter, r *http.Request) {
	status, err := gatherStatus()
	if err != nil {
		log.Printf("gatherStatus failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    fmt.Fprintf(w, "<html><body><pre>%s</pre></body></html>", html.EscapeString(status))
}


func main () {
	vars, err := godotenv.Read("/home/saturn-svc/.config/ripper/env.sh")
	if err != nil{
		log.Fatal(err)
	}
	statusTmpPath, ok := vars["STATUS_TMP"]
	if !ok {
		log.Fatal("STATUS_TMP not set in config")
	}

	dir := filepath.Dir(statusTmpPath)
	statusFile = filepath.Base(statusTmpPath)

	statusRoot, err = os.OpenRoot(dir)
	if err != nil {
        log.Fatalf("opening status root %q: %v", dir, err)
	}
	defer statusRoot.Close()
	
	http.HandleFunc("GET /{$}", statusHandler)
	srv := &http.Server{
		Addr: ":9511",
		ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe()) 
}