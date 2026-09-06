package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	rolloutHashRegex  = regexp.MustCompile(`"(?:rollout_hash|__spin_r)":\s*"?(\d+)"?`)
	hasteSessionRegex = regexp.MustCompile(`"(?:haste_session|__hs)":\s*"([^",\\]+)"`)
	hsiRegex          = regexp.MustCompile(`"(?:hsi|__hsi)":\s*"?(\d+)"?`)
	bloksVersionRegex = regexp.MustCompile(`"(?:versioningID|bloks_version)":\s*"([a-f0-9]{64})"`)
	asbdRegex         = regexp.MustCompile(`['"]X-ASBD-ID['"]:\s*['"](\d+)['"]`)

	// Regexes to extract/replace in internal/extractors/instagram/util.go
	utilRolloutRegex = regexp.MustCompile(`gqlRolloutHash\s*=\s*"([^"]*)"`)
	utilHiddenRegex  = regexp.MustCompile(`gqlHiddenState\s*=\s*"([^"]*)"`)
	utilBloksRegex   = regexp.MustCompile(`gqlBloksVersion\s*=\s*"([^"]*)"`)
	utilAsbdRegex    = regexp.MustCompile(`gqlAsbdID\s*=\s*"([^"]*)"`)
	utilHsiRegex     = regexp.MustCompile(`sessionInternalID\s*=\s*"([^"]*)"`)
)

func main() {
	fmt.Println("[updater] Checking latest Instagram GQL constants...")

	targetFile := findUtilFile()
	if targetFile == "" {
		fmt.Println("[updater] Warning: internal/extractors/instagram/util.go not found, skipping update")
		return
	}

	contentBytes, err := os.ReadFile(targetFile)
	if err != nil {
		fmt.Printf("[updater] Warning: failed to read %s: %v\n", targetFile, err)
		return
	}
	content := string(contentBytes)

	currRollout := extractString(utilRolloutRegex, content)
	currHaste := extractString(utilHiddenRegex, content)
	currBloks := extractString(utilBloksRegex, content)
	currAsbd := extractString(utilAsbdRegex, content)

	var (
		newRollout string
		newHaste   string
		newBloks   string
		newHsi     string
		newAsbd    string
	)

	html, err := fetchInstagramHTML()
	if err != nil {
		fmt.Printf("[updater] Warning: could not fetch Instagram homepage: %v\n", err)
	} else {
		if m := rolloutHashRegex.FindStringSubmatch(html); len(m) > 1 {
			newRollout = m[1]
		}
		if m := hasteSessionRegex.FindStringSubmatch(html); len(m) > 1 {
			newHaste = m[1]
		}
		if m := hsiRegex.FindStringSubmatch(html); len(m) > 1 {
			newHsi = m[1]
		}
		if m := bloksVersionRegex.FindStringSubmatch(html); len(m) > 1 {
			newBloks = m[1]
		}
	}

	if asbd := fetchLatestASBD(); asbd != "" {
		newAsbd = asbd
	}

	hasChange := false
	if newRollout != "" && newRollout != currRollout {
		fmt.Printf("[updater] rollout_hash: %s -> %s\n", currRollout, newRollout)
		hasChange = true
	}
	if newHaste != "" && newHaste != currHaste {
		fmt.Printf("[updater] haste_session: %s -> %s\n", currHaste, newHaste)
		hasChange = true
	}
	if newBloks != "" && newBloks != currBloks {
		fmt.Printf("[updater] bloks_version: %s -> %s\n", currBloks, newBloks)
		hasChange = true
	}
	if newAsbd != "" && newAsbd != currAsbd {
		fmt.Printf("[updater] asbd_id: %s -> %s\n", currAsbd, newAsbd)
		hasChange = true
	}

	if !hasChange {
		fmt.Println("[updater] Instagram fingerprint constants are already up to date")
		return
	}

	// Apply updates
	if newRollout != "" {
		content = utilRolloutRegex.ReplaceAllString(content, fmt.Sprintf(`gqlRolloutHash  = "%s"`, newRollout))
	}
	if newHaste != "" {
		content = utilHiddenRegex.ReplaceAllString(content, fmt.Sprintf(`gqlHiddenState  = "%s"`, newHaste))
	}
	if newBloks != "" {
		content = utilBloksRegex.ReplaceAllString(content, fmt.Sprintf(`gqlBloksVersion = "%s"`, newBloks))
	}
	if newAsbd != "" {
		content = utilAsbdRegex.ReplaceAllString(content, fmt.Sprintf(`gqlAsbdID       = "%s"`, newAsbd))
	}
	if newHsi != "" {
		content = utilHsiRegex.ReplaceAllString(content, fmt.Sprintf(`sessionInternalID     = "%s"`, newHsi))
	}

	if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
		fmt.Printf("[updater] Warning: failed to write updated %s: %v\n", targetFile, err)
		return
	}
	fmt.Printf("[updater] Successfully updated %s with latest Instagram fingerprint constants\n", targetFile)
}

func extractString(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func findUtilFile() string {
	candidates := []string{
		"internal/extractors/instagram/util.go",
		"/app/internal/extractors/instagram/util.go",
		"../internal/extractors/instagram/util.go",
		"../../internal/extractors/instagram/util.go",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	dir, err := os.Getwd()
	if err == nil {
		for i := 0; i < 4; i++ {
			testPath := filepath.Join(dir, "internal/extractors/instagram/util.go")
			if _, err := os.Stat(testPath); err == nil {
				return testPath
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return ""
}

func fetchInstagramHTML() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fetchLatestASBD() string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/yt-dlp/yt-dlp/master/yt_dlp/extractor/instagram.py", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	m := asbdRegex.FindStringSubmatch(string(body))
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
