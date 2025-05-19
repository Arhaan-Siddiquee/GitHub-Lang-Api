package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
)

type Repo struct {
	LanguagesURL string `json:"languages_url"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	username := r.URL.Query().Get("username")
	if username == "" {
		http.Error(w, "Missing username", http.StatusBadRequest)
		return
	}

	reposURL := fmt.Sprintf("https://api.github.com/users/%s/repos", username)
	resp, err := http.Get(reposURL)
	if err != nil {
		http.Error(w, "Failed to fetch repos", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		http.Error(w, "GitHub user not found", http.StatusNotFound)
		return
	}

	body, _ := io.ReadAll(resp.Body)

	var repos []Repo
	err = json.Unmarshal(body, &repos)
	if err != nil {
		http.Error(w, "Failed to parse repo JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	languageStats := make(map[string]float64)
	for _, repo := range repos {
		langResp, err := http.Get(repo.LanguagesURL)
		if err != nil {
			continue
		}
		defer langResp.Body.Close()

		var langData map[string]float64
		langBody, _ := io.ReadAll(langResp.Body)
		json.Unmarshal(langBody, &langData)

		for lang, bytes := range langData {
			languageStats[lang] += bytes
		}
	}

	var total float64
	for _, bytes := range languageStats {
		total += bytes
	}

	languagePercentages := make(map[string]float64)
	for lang, bytes := range languageStats {
		percent := (bytes / total) * 100
		languagePercentages[lang] = math.Round(percent*100) / 100 
	}

	json.NewEncoder(w).Encode(languagePercentages)
}

func main() {
	http.HandleFunc("/languages", handler)
	fmt.Println("🚀 Server started at http://localhost:8080/languages?username=Arhaan-Siddiquee")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
