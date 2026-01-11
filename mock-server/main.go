// mock-server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	// Handler ini akan merespons secepat mungkin
	http.HandleFunc("/mock", func(w http.ResponseWriter, r *http.Request) {
		// Kita tidak melakukan apa-apa, hanya kirim OK
		// Ini adalah respons tercepat yang bisa diberikan Go
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"mock_ok"}`)
	})

	port := "9090"
	log.Printf("Starting fast mock server on %s:%s", "localhost", port)

	// Jalankan di port yang berbeda dari gateway Anda
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil); err != nil {
		log.Fatalf("Could not start mock server: %s\n", err)
	}
}
