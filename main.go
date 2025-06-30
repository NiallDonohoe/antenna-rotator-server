package main

import (
	"fmt"
	"log"
	"net/http"
)

type Heading struct {
	Current int    `json:"current"`
	Desired string `json:"desired"`
}

func main() {
	http.HandleFunc("/heading", headingHandler)
	http.HandleFunc("/heading/", headingHandler)

	fmt.Println("Server is running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func headingHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		handleGetHeading(w, r)
	case "POST":
		handlePostHeading(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetHeading(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Getting Heading")
}

func handlePostHeading(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Updating Heading")
}
