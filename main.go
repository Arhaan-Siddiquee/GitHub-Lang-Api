package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"time"
)

type Repo struct {
	LanguagesURL string `json:"languages_url"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

type LanguageStats struct {
	Language string  `json:"language"`
	Percent  float64 `json:"percent"`
	Bytes    int64   `json:"bytes"`
}

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	})
}

func fetchGitHubData(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Add("User-Agent", "GitHub-Language-Analyzer")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Add("Authorization", fmt.Sprintf("token %s", token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "GET" {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		writeJSONError(w, "Username parameter is required", http.StatusBadRequest)
		return
	}

	reposURL := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100", username)
	reposBody, err := fetchGitHubData(reposURL)
	if err != nil {
		log.Printf("Error fetching repositories: %v", err)
		writeJSONError(w, fmt.Sprintf("Failed to fetch repositories: %v", err), http.StatusInternalServerError)
		return
	}

	var repos []Repo
	if err := json.Unmarshal(reposBody, &repos); err != nil {
		log.Printf("Error parsing repositories: %v", err)
		writeJSONError(w, "Failed to parse repository data", http.StatusInternalServerError)
		return
	}

	if len(repos) == 0 {
		writeJSONError(w, "No repositories found for this user", http.StatusNotFound)
		return
	}

	languageStats := make(map[string]int64)
	for _, repo := range repos {
		if repo.LanguagesURL == "" {
			continue
		}

		langBody, err := fetchGitHubData(repo.LanguagesURL)
		if err != nil {
			log.Printf("Error fetching languages for repo: %v", err)
			continue
		}

		var langData map[string]int64
		if err := json.Unmarshal(langBody, &langData); err != nil {
			log.Printf("Error parsing languages: %v", err)
			continue
		}

		for lang, bytes := range langData {
			languageStats[lang] += bytes
		}
	}

	var totalBytes int64
	for _, bytes := range languageStats {
		totalBytes += bytes
	}

	if totalBytes == 0 {
		writeJSONError(w, "No language data found in repositories", http.StatusNotFound)
		return
	}

	var result []LanguageStats
	for lang, bytes := range languageStats {
		percent := (float64(bytes) / float64(totalBytes)) * 100
		result = append(result, LanguageStats{
			Language: lang,
			Percent:  math.Round(percent*100) / 100,
			Bytes:    bytes,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Percent > result[j].Percent
	})

	w.Header().Set("X-RateLimit-Info", "GitHub API rate limits apply. Add GITHUB_TOKEN for higher limits.")

	json.NewEncoder(w).Encode(result)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Starting GitHub Language Analyzer Server")
	log.Printf("Using port: %s", port)
	if os.Getenv("GITHUB_TOKEN") == "" {
		log.Println("Warning: Running without GITHUB_TOKEN - limited to 60 requests/hour")
	} else {
		log.Println("Using GITHUB_TOKEN for authentication")
	}
	log.Printf("Access the endpoint at: http://localhost:%s/languages?username=USERNAME", port)

	http.HandleFunc("/languages", handler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}