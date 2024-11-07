package main

import (
	"fmt"
	"io"
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

// Structs for front matter, configuration, and template caching
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

type TemplateData struct {
	Site    Config      // Site-wide config data (e.g., title, baseURL)
	Page    FrontMatter // Page-specific front matter
	Content string      // HTML content of the page
}

type TemplateCache struct {
	templates map[string]*template.Template
	partials  *template.Template
}

type Post struct {
	Title       string
	Description string
	Date        time.Time
	Content     string
}

// Main entry point
func main() {
	config, err := loadConfig("config.toml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	themeDir := filepath.Join("themes", config.Theme)
	postsDir := "./content/"
	publicDir := "./public/"

	// Create output directory
	if err := os.MkdirAll(publicDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create public directory: %v", err)
	}

	// Load templates
	cache, err := loadTemplates(themeDir)
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	// Process all files in the content directory, including subdirectories
	var wg sync.WaitGroup
	err = filepath.Walk(postsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error walking through content directory: %v", err)
			return err
		}

		// Process only markdown files
		if !info.IsDir() && filepath.Ext(path) == ".md" {
			wg.Add(1)
			go func(filePath string) {
				defer wg.Done()
				if err := processMarkdownFile(filePath, publicDir, themeDir, config, cache); err != nil {
					log.Printf("Failed to process file %s: %v", filePath, err)
				}
			}(path)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Error walking the content directory: %v", err)
	}

	// Wait for all processing to complete
	wg.Wait()

	// Copy static files (e.g., CSS, JS, images)
	copyStaticFiles(themeDir, publicDir)
}

// loadConfig reads the configuration file
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

// Template handling

// Function to load templates with helper functions registered
func loadTemplates(themeDir string) (*TemplateCache, error) {
	cache := &TemplateCache{
		templates: make(map[string]*template.Template),
		partials:  new(template.Template),
	}
	layoutsDir := filepath.Join(themeDir, "layouts")

	// Custom function map with helpers like partial
	funcMap := template.FuncMap{
		"partial": partialFunc(themeDir),
		"title":   strings.Title,
	}

	// Load and parse partials
	partialsGlob := filepath.Join(layoutsDir, "partials", "*.html")
	if partialFiles, err := filepath.Glob(partialsGlob); err == nil && len(partialFiles) > 0 {
		partials, err := template.New("partials").Funcs(funcMap).ParseGlob(partialsGlob)
		if err != nil {
			return nil, fmt.Errorf("failed to parse partial templates: %w", err)
		}
		cache.partials = partials
	} else {
		log.Printf("No partial templates found in %s, proceeding without them.", partialsGlob)
	}

	// Load other templates (e.g., base.html) with funcMap applied
	err := filepath.Walk(layoutsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".html") {
			return err
		}

		templateType := inferTemplateType(path, layoutsDir)

		// Apply the funcMap to each template
		tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).ParseFiles(path)
		if err != nil {
			log.Printf("Skipping template %s due to parsing error: %v", path, err)
			return nil // Continue without halting on template parse errors
		}

		// Register the template
		cache.templates[templateType] = tmpl
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	return cache, nil
}

// partialFunc returns a function to render partials
func partialFunc(themeDir string) func(name string, data interface{}) (string, error) {
	return func(name string, data interface{}) (string, error) {
		var buf strings.Builder
		partialPath := filepath.Join(themeDir, "layouts", "partials", name)
		tmpl, err := template.ParseFiles(partialPath)
		if err != nil {
			return "", fmt.Errorf("failed to load partial %s: %w", name, err)
		}
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", fmt.Errorf("failed to execute partial %s: %w", name, err)
		}
		return buf.String(), nil
	}
}

// inferTemplateType infers template type from its path
func inferTemplateType(path, layoutsDir string) string {
	relPath, _ := filepath.Rel(layoutsDir, path)
	if strings.HasPrefix(relPath, "taxonomy/") {
		return relPath
	}
	return strings.TrimSuffix(filepath.Base(path), ".html")
}

// Content processing

// processMarkdownFile processes a single markdown file and writes it to the public directory
func processMarkdownFile(filePath, outputDir, themeDir string, config Config, cache *TemplateCache) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	frontMatter, markdownContent, err := extractFrontMatter(content)
	if err != nil {
		log.Printf("Warning: Malformed front matter in %s: %v", filePath, err)
		frontMatter = FrontMatter{}
	}

	htmlContent, err := convertMarkdownToHTML(markdownContent)
	if err != nil {
		return fmt.Errorf("failed to convert Markdown: %w", err)
	}

	// Determine the output path based on the folder structure
	relPath, _ := filepath.Rel("./content", filePath)
	relPath = strings.TrimSuffix(relPath, filepath.Ext(relPath)) + ".html"
	outputPath := filepath.Join(outputDir, relPath)

	// Create directories in the output path if needed
	if err := os.MkdirAll(filepath.Dir(outputPath), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Render the HTML using the base template and write the output
	if err := writeHTMLFile(outputPath, frontMatter, htmlContent, themeDir, config, cache); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	return nil
}

// extractFrontMatter extracts YAML front matter from markdown content
func extractFrontMatter(content []byte) (FrontMatter, []byte, error) {
	var fm FrontMatter
	contentStr := string(content)

	if strings.HasPrefix(contentStr, "---") {
		parts := strings.SplitN(contentStr, "\n---\n", 2)
		if len(parts) == 2 {
			meta := strings.Trim(parts[0], "-+ \n")
			body := parts[1]
			if err := yaml.Unmarshal([]byte(meta), &fm); err != nil {
				return fm, nil, fmt.Errorf("failed to parse front matter: %w", err)
			}
			return fm, []byte(body), nil
		}
	}
	return fm, content, nil
}

// convertMarkdownToHTML converts markdown content to HTML
func convertMarkdownToHTML(content []byte) (string, error) {
	md := goldmark.New()
	var buf strings.Builder
	if err := md.Convert(content, &buf); err != nil {
		return "", fmt.Errorf("failed to convert markdown to HTML: %w", err)
	}
	return buf.String(), nil
}

// writeHTMLFile writes the rendered HTML to a file
func writeHTMLFile(outputPath string, fm FrontMatter, htmlContent, themeDir string, config Config, cache *TemplateCache) error {
	tmpl, exists := cache.templates["base"]
	if !exists {
		return fmt.Errorf("failed to find base template")
	}

	// Prepare data for template rendering
	data := TemplateData{
		Site:    config,
		Page:    fm,
		Content: htmlContent,
	}

	// Create file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outputFile.Close()

	// Execute template and write to file
	if err := tmpl.Execute(outputFile, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}
