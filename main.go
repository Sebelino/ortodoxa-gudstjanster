package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
)

//go:embed templates/index.html
var templates embed.FS

const defaultURL = "https://www.ortodox-finsk.se/kalender/"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/services", handleServices)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := templates.ReadFile("templates/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleServices(w http.ResponseWriter, r *http.Request) {
	var allServices []ChurchService

	// Fetch from Finska Ortodoxa Församlingen
	if services, err := FetchCalendar(defaultURL); err == nil {
		allServices = append(allServices, services...)
	}

	// Fetch from St. Georgios Cathedral (OCR-based)
	if services, err := FetchGomosCalendar(); err == nil {
		allServices = append(allServices, services...)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(allServices)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
