package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

const defaultURL = "https://www.ortodox-finsk.se/kalender/"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/services", handleServices)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleServices(w http.ResponseWriter, r *http.Request) {
	services, err := FetchCalendar(defaultURL)
	if err != nil {
		http.Error(w, "Failed to fetch calendar: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(services)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
