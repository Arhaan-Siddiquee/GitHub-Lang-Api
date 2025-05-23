package main

import (
	"encoding/json"
	"flag"
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

func getLanguageStats(username string) ([]LanguageStats, error) {
	reposURL := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100", username)
	reposBody, err := fetchGitHubData(reposURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repositories: %v", err)
	}

	var repos []Repo
	if err := json.Unmarshal(reposBody, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repository data: %v", err)
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("no repositories found for user %s", username)
	}

	languageStats := make(map[string]int64)
	for _, repo := range repos {
		if repo.LanguagesURL == "" {
			continue
		}

		langBody, err := fetchGitHubData(repo.LanguagesURL)
		if err != nil {
			log.Printf("Warning: Error fetching languages for repo: %v", err)
			continue
		}

		var langData map[string]int64
		if err := json.Unmarshal(langBody, &langData); err != nil {
			log.Printf("Warning: Error parsing languages: %v", err)
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
		return nil, fmt.Errorf("no language data found in repositories")
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

	return result, nil
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

	result, err := getLanguageStats(username)
	if err != nil {
		log.Printf("Error getting language stats: %v", err)
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-RateLimit-Info", "GitHub API rate limits apply. Add GITHUB_TOKEN for higher limits.")
	json.NewEncoder(w).Encode(result)
}

func printStatsCLI(username string) {
	fmt.Printf("Fetching language stats for GitHub user: %s\n", username)
	
	result, err := getLanguageStats(username)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Println("\nLanguage Statistics:")
	fmt.Println("----------------------------------------")
	fmt.Printf("%-20s %10s %10s\n", "LANGUAGE", "PERCENT", "BYTES")
	fmt.Println("----------------------------------------")
	for _, stat := range result {
		fmt.Printf("%-20s %9.2f%% %10d\n", stat.Language, stat.Percent, stat.Bytes)
	}
	fmt.Println("----------------------------------------")
	
	var totalBytes int64
	for _, stat := range result {
		totalBytes += stat.Bytes
	}
	fmt.Printf("%-20s %10s %10d\n", "TOTAL", "", totalBytes)
}

func main() {
	// Parse CLI flags
	cliMode := flag.Bool("cli", false, "Run in CLI mode")
	username := flag.String("user", "", "GitHub username to analyze")
	flag.Parse()

	if *cliMode {
		if *username == "" {
			fmt.Println("Error: username is required in CLI mode")
			fmt.Println("Usage: program -cli -user <username>")
			os.Exit(1)
		}
		printStatsCLI(*username)
		return
	}

	// Otherwise start HTTP server
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
	log.Println("Or use CLI mode: program -cli -user USERNAME")

	http.HandleFunc("/languages", handler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}