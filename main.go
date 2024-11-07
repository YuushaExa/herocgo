package main

import (
	"fmt"
	"html"
	"io"          // Ensure io is imported for io.Copy
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
	"github.com/yuin/goldmark"
)

// Structs for front matter and configuration
type FrontMatter struct {
	Title       string `yaml:"title" toml:"title"`
	Description string `yaml:"description" toml:"description"`
	Date        string `yaml:"date" toml:"date"`
}

type Config struct {
	Title   string `toml:"title"`
	BaseURL string `toml:"baseURL"`
	Theme   string `toml:"theme"`
}

func main() {
	// Load configuration
	config, err := loadConfig("config.toml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate configuration
	themeDir := filepath.Join("themes", config.Theme)
	if _, err := os.Stat(themeDir); os.IsNotExist(err) {
		log.Fatalf("Theme directory does not exist: %s", themeDir)
	}

	postsDir := "./content/"
	publicDir := "./public/"

	// Create output directory
	if err := os.MkdirAll(publicDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create public directory: %v", err)
	}

	// Prepare build statistics
	var totalPages, nonPageFiles int
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()

	files, err := os.ReadDir(postsDir)
	if err != nil {
		log.Fatalf("Failed to read content directory: %v", err)
	}

	// Process each file concurrently
	for _, file := range files {
		wg.Add(1)
		go func(file os.DirEntry) {
			defer wg.Done()
			if filepath.Ext(file.Name()) == ".md" {
				if err := processMarkdownFile(filepath.Join(postsDir, file.Name()), publicDir, themeDir); err != nil {
					log.Printf("Failed to process file %s: %v", file.Name(), err)
				} else {
					mu.Lock()
					totalPages++
					mu.Unlock()
				}
			} else {
				mu.Lock()
				nonPageFiles++
				mu.Unlock()
			}
		}(file)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Copy theme static files to public directory
	if err := copyStaticFiles(themeDir, publicDir); err != nil {
		log.Printf("Failed to copy static files: %v", err)
	}

	// Print build statistics
	fmt.Println("--- Build Statistics ---")
	fmt.Printf("Total Pages: %d\n", totalPages)
	fmt.Printf("Non-page Files: %d\n", nonPageFiles)
	fmt.Printf("Total Build Time: %v\n", time.Since(start))
}

// loadConfig reads and parses the configuration file
func loadConfig(path string) (Config, error) {
	var config Config
	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("could not read config: %w", err)
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("could not parse config: %w", err)
	}
	return config, nil
}

// processMarkdownFile reads a Markdown file, parses front matter, converts content, and writes an HTML file
func processMarkdownFile(filePath, outputDir, themeDir string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	frontMatter, markdownContent, err := extractFrontMatter(content)
	if err != nil {
		log.Printf("Warning: Malformed front matter in %s: %v", filePath, err)
		// Set front matter to default values if parsing fails
		frontMatter = FrontMatter{}
	}

	htmlContent, err := convertMarkdownToHTML(markdownContent)
	if err != nil {
		return fmt.Errorf("failed to convert Markdown: %w", err)
	}

		outputFileName := filepath.Base(filePath[:len(filePath)-len(filepath.Ext(filePath))]) + ".html"
	outputPath := filepath.Join(outputDir, outputFileName)

	if err := writeHTMLFile(outputPath, frontMatter, htmlContent, themeDir); err != nil {
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
	return fm, content, nil
}

// convertMarkdownToHTML converts Markdown to HTML using goldmark
func convertMarkdownToHTML(content []byte) (string, error) {
	md := goldmark.New()
	var buf strings.Builder
	if err := md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// writeHTMLFile creates an HTML file with escaped title and description to prevent XSS
func writeHTMLFile(outputPath string, fm FrontMatter, htmlContent, themeDir string) error {
    // Paths for layout and partials
    baseTemplatePath := filepath.Join(themeDir, "layouts", "themes", "default", "base.html")
    partialTemplatePath := filepath.Join(themeDir, "layouts", "partials", "head.html")

    // Parse the base template and partial templates together
    tmpl, err := template.New("base").ParseFiles(baseTemplatePath, partialTemplatePath)
    if err != nil {
        return fmt.Errorf("failed to load templates: %w", err)
    }

    // Create the output HTML file
    file, err := os.Create(outputPath)
    if err != nil {
        return fmt.Errorf("failed to create HTML file: %w", err)
    }
    defer file.Close()

    // Define the data structure to pass into the template
    data := struct {
        Title       string
        Description string
        Content     string
    }{
        Title:       html.EscapeString(fm.Title),
        Description: html.EscapeString(fm.Description),
        Content:     htmlContent,
    }

    // Execute the base template with the provided data
    if err := tmpl.ExecuteTemplate(file, "base", data); err != nil {
        return fmt.Errorf("failed to execute template: %w", err)
    }

    return nil
}




// copyStaticFiles copies static files from the theme directory to the public directory
func copyStaticFiles(themeDir, publicDir string) error {
	staticDir := filepath.Join(themeDir, "static")
	return filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			dest := filepath.Join(publicDir, strings.TrimPrefix(path, staticDir))
			if err := os.MkdirAll(filepath.Dir(dest), os.ModePerm); err != nil {
				return err
			}
			if _, err := copyFile(path, dest); err != nil {
				return err
			}
		}
		return nil
	})
}

// copyFile is a helper to copy files from source to destination
func copyFile(src, dest string) (int64, error) {
	sourceFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer destFile.Close()

	return io.Copy(destFile, sourceFile)
}
