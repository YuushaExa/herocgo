package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
	"github.com/yuin/goldmark"
)

type FrontMatter struct {
	Title       string `yaml:"title" toml:"title"`
	Description string `yaml:"description" toml:"description"`
	Date        string `yaml:"date" toml:"date"`
}

func main() {
	postsDir := "./posts/"
	publicDir := "./public/"
	var totalPages, nonPageFiles, staticFiles int
	var mu sync.Mutex  // Mutex to protect shared counters
	var wg sync.WaitGroup

	start := time.Now()

	// Create output directory
	if err := os.MkdirAll(publicDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create public directory: %v", err)
	}

	// Read files in the posts directory
	files, err := os.ReadDir(postsDir)
	if err != nil {
		log.Fatalf("Failed to read posts directory: %v", err)
	}

	// Process each file concurrently
	for _, file := range files {
		wg.Add(1)  // Increment WaitGroup counter
		go func(file os.DirEntry) {
			defer wg.Done() // Decrement WaitGroup counter when done

			// Process only Markdown files
			if filepath.Ext(file.Name()) == ".md" {
				if err := processMarkdownFile(filepath.Join(postsDir, file.Name()), publicDir); err != nil {
					log.Printf("Failed to process file %s: %v", file.Name(), err)
				} else {
					// Lock the counter update to avoid race conditions
					mu.Lock()
					totalPages++
					mu.Unlock()
				}
			} else {
				// Lock the counter update to avoid race conditions
				mu.Lock()
				nonPageFiles++
				mu.Unlock()
			}
		}(file)  // Pass file as argument to the goroutine
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Calculate total build time
	totalBuildTime := time.Since(start)

	// Print build statistics
	fmt.Println("--- Build Statistics ---")
	fmt.Printf("Total Pages: %d\n", totalPages)
	fmt.Printf("Non-page Files: %d\n", nonPageFiles)
	fmt.Printf("Static Files: %d\n", staticFiles) // Update if you add static files processing
	fmt.Printf("Total Build Time: %v\n", totalBuildTime)
}

// processMarkdownFile reads a Markdown file, parses front matter, converts content, and writes an HTML file
func processMarkdownFile(filePath, outputDir string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	frontMatter, markdownContent, err := extractFrontMatter(content)
	if err != nil {
		log.Printf("Warning: Malformed front matter in %s: %v", filePath, err)
	}

	htmlContent, err := convertMarkdownToHTML(markdownContent)
	if err != nil {
		return fmt.Errorf("failed to convert Markdown: %w", err)
	}

	outputFileName := filepath.Base(filePath[:len(filePath)-len(filepath.Ext(filePath))]) + ".html"
	outputPath := filepath.Join(outputDir, outputFileName)

	if err := writeHTMLFile(outputPath, frontMatter, htmlContent); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	return nil
}

// extractFrontMatter separates the front matter from the Markdown content
func extractFrontMatter(content []byte) (FrontMatter, []byte, error) {
	var fm FrontMatter
	contentStr := string(content)

	if strings.HasPrefix(contentStr, "---") || strings.HasPrefix(contentStr, "+++") {
		var parts []string
		if strings.HasPrefix(contentStr, "---") {
			parts = strings.SplitN(contentStr, "\n---\n", 2)
		} else {
			parts = strings.SplitN(contentStr, "\n+++\n", 2)
		}

		if len(parts) == 2 {
			meta := strings.Trim(parts[0], "-+ \n")
			body := parts[1]

			if strings.HasPrefix(contentStr, "---") {
				if err := yaml.Unmarshal([]byte(meta), &fm); err != nil {
					return fm, []byte(body), fmt.Errorf("failed to parse YAML front matter: %w", err)
				}
			} else {
				if err := toml.Unmarshal([]byte(meta), &fm); err != nil {
					return fm, []byte(body), fmt.Errorf("failed to parse TOML front matter: %w", err)
				}
			}
			return fm, []byte(body), nil
		}
		return fm, content, fmt.Errorf("no valid front matter delimiter found")
	}
	return fm, content, nil // No front matter found
}

// convertMarkdownToHTML converts Markdown to HTML using goldmark
func convertMarkdownToHTML(content []byte) (string, error) {
	md := goldmark.New() // Default markdown processor without additional extensions

	var buf strings.Builder
	if err := md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// writeHTMLFile creates an HTML file with escaped title and description to prevent XSS
func writeHTMLFile(outputPath string, fm FrontMatter, htmlContent string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %w", err)
	}
	defer file.Close()

	// Escape title and description for security
	escapedTitle := html.EscapeString(fm.Title)
	escapedDescription := html.EscapeString(fm.Description)

	_, err = file.WriteString(fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <meta name="description" content="%s">
</head>
<body>
%s
</body>
</html>`, escapedTitle, escapedDescription, htmlContent))

	if err != nil {
		return fmt.Errorf("failed to write to HTML file: %w", err)
	}
	return nil
}
