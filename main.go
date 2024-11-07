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

	// Process markdown files
	var wg sync.WaitGroup
	files, err := os.ReadDir(postsDir)
	if err != nil {
		log.Fatalf("Failed to read content directory: %v", err)
	}

	for _, file := range files {
		wg.Add(1)
		go func(file os.DirEntry) {
			defer wg.Done()
			if filepath.Ext(file.Name()) == ".md" {
				if err := processMarkdownFile(filepath.Join(postsDir, file.Name()), publicDir, themeDir, config); err != nil {
					log.Printf("Failed to process file %s: %v", file.Name(), err)
				}
			}
		}(file)
	}

	// Wait for all processing to complete
	wg.Wait()

	// Render taxonomies
	taxonomies := map[string][]string{"tags": {"tag1", "tag2"}, "categories": {"cat1", "cat2"}} // Example taxonomy data
	postsByTerm := map[string]map[string][]Post{"tags": {}, "categories": {}} // Example post data
	renderTaxonomies(cache, taxonomies, postsByTerm, publicDir)

	// Copy static files
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
// Function to load templates with helper functions registered
func loadTemplates(themeDir string) (*TemplateCache, error) {
	cache := &TemplateCache{
		templates: make(map[string]*template.Template),
		partials:  new(template.Template),
	}
	layoutsDir := filepath.Join(themeDir, "layouts")

	// Custom function map with helpers like partial, partialCached, and title
	funcMap := template.FuncMap{
		"partial":       partialFunc(themeDir),
		"partialCached": partialCachedFunc(themeDir),
		"title":         strings.Title,
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

	// Load other templates (e.g., base.html)
	err := filepath.Walk(layoutsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".html") {
			return err
		}

		templateType := inferTemplateType(path, layoutsDir)
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

// partialCachedFunc is similar to partialFunc, but implements caching for frequently used partials
func partialCachedFunc(themeDir string) func(name string, data interface{}) (string, error) {
	cache := make(map[string]*template.Template)
	return func(name string, data interface{}) (string, error) {
		var buf strings.Builder
		partialPath := filepath.Join(themeDir, "layouts", "partials", name)

		tmpl, ok := cache[partialPath]
		if !ok {
			var err error
			tmpl, err = template.ParseFiles(partialPath)
			if err != nil {
				return "", fmt.Errorf("failed to load cached partial %s: %w", name, err)
			}
			cache[partialPath] = tmpl
		}

		if err := tmpl.Execute(&buf, data); err != nil {
			return "", fmt.Errorf("failed to execute cached partial %s: %w", name, err)
		}
		return buf.String(), nil
	}
}

func inferTemplateType(path, layoutsDir string) string {
	relPath, _ := filepath.Rel(layoutsDir, path)
	if strings.HasPrefix(relPath, "taxonomy/") {
		return relPath
	}
	return strings.TrimSuffix(filepath.Base(path), ".html")
}

// Content processing

func processMarkdownFile(filePath, outputDir, themeDir string, config Config) error {
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

	outputFileName := filepath.Base(filePath[:len(filePath)-len(filepath.Ext(filePath))]) + ".html"
	outputPath := filepath.Join(outputDir, outputFileName)

	// Pass in themeDir and config as additional arguments
	if err := writeHTMLFile(outputPath, frontMatter, htmlContent, themeDir, config); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	return nil
}


func extractFrontMatter(content []byte) (FrontMatter, []byte, error) {
	var fm FrontMatter
	contentStr := string(content)

	if strings.HasPrefix(contentStr, "---") {
		parts := strings.SplitN(contentStr, "\n---\n", 2)
		if len(parts) == 2 {
			meta := strings.Trim(parts[0], "-+ \n")
			body := parts[1]
			if err := yaml.Unmarshal([]byte(meta), &fm); err != nil {
				return fm, []byte(body), fmt.Errorf("failed to parse YAML front matter: %w", err)
			}
			return fm, []byte(body), nil
		}
	}
	return fm, content, nil
}

func convertMarkdownToHTML(content []byte) (string, error) {
	md := goldmark.New()
	var buf strings.Builder
	if err := md.Convert(content, &buf); err != nil {
		return "", fmt.Errorf("failed to convert markdown: %w", err)
	}
	return buf.String(), nil
}

func writeHTMLFile(outputPath string, fm FrontMatter, htmlContent, themeDir string, config Config) error {
	tmplPath := filepath.Join(themeDir, "layouts", "base.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %w", err)
	}
	defer file.Close()

	// Prepare the data to pass into the template
	data := TemplateData{
		Site:    config,       // Pass the config here
		Page:    fm,           // Front matter for the current page
		Content: htmlContent,  // Converted HTML content
	}

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}
	return nil
}

// Taxonomy and static file handling

func renderTaxonomies(cache *TemplateCache, taxonomies map[string][]string, postsByTerm map[string]map[string][]Post, publicDir string) {
	// Implementation for rendering taxonomies goes here
	// Example: Create a taxonomy page based on categories or tags
}

func copyStaticFiles(themeDir, publicDir string) {
	staticDir := filepath.Join(themeDir, "static")
	if err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Copy file to the public directory
		destPath := filepath.Join(publicDir, filepath.Base(path))
		inputFile, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open static file: %w", err)
		}
		defer inputFile.Close()

		outputFile, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("failed to create static file in public directory: %w", err)
		}
		defer outputFile.Close()

		if _, err := io.Copy(outputFile, inputFile); err != nil {
			return fmt.Errorf("failed to copy static file: %w", err)
		}

		return nil
	}); err != nil {
		log.Printf("Error copying static files: %v", err)
	}
}
