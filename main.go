package main

import (
	"os"	
	"log"
	"fmt"
	"html"
	"net/http"
	"github.com/joho/godotenv"
)

var statusTmpPath string

func gatherStatus() (string, error) {
	statusFrame, err := os.ReadFile(statusTmpPath)
	if err != nil {
		return "", err
	}

	statusString := string(statusFrame)

	return statusString, nil
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
	var ok bool
	statusTmpPath, ok = vars["STATUS_TMP"]
	if !ok {
		log.Fatal("STATUS_TMP not set in config")
	}
	
	http.HandleFunc("GET /{$}", statusHandler)
	log.Fatal(http.ListenAndServe(":9511", nil)) 
}