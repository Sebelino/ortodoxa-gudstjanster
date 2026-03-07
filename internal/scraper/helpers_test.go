package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchURLNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	_, err := fetchURL(context.Background(), srv.URL)
	if err == nil {
		t.Error("fetchURL should return error for 404 response")
	}
}

func TestFetchURL200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	data, err := fetchURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchURL should succeed for 200: %v", err)
	}
	if string(data) != "ok" {
		t.Errorf("fetchURL = %q, want %q", string(data), "ok")
	}
}

func TestFetchDocumentNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("<html>error</html>"))
	}))
	defer srv.Close()

	_, err := fetchDocument(context.Background(), srv.URL)
	if err == nil {
		t.Error("fetchDocument should return error for 500 response")
	}
}

func TestFetchDocument200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer srv.Close()

	doc, err := fetchDocument(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchDocument should succeed for 200: %v", err)
	}
	if doc.Find("body").Text() != "hello" {
		t.Errorf("body text = %q, want %q", doc.Find("body").Text(), "hello")
	}
}
