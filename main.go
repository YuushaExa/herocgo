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
func loadTemplates(themeDir string) (*TemplateCache, error) {
    cache := &TemplateCache{
        templates: make(map[string]*template.Template),
        partials:  new(template.Template),
    }
    layoutsDir := filepath.Join(themeDir, "layouts")

    // Define funcMap with custom functions like partial and title
    funcMap := template.FuncMap{
        "partial":       partialFunc(themeDir),
        "partialCached": partialCachedFunc(themeDir),
        "title":         strings.Title,
    }

    // Load and parse partial templates with funcMap applied
    partialsGlob := filepath.Join(layoutsDir, "partials", "*.html")
    partialFiles, err := filepath.Glob(partialsGlob)
    if err != nil {
        return nil, fmt.Errorf("failed to read partials: %w", err)
    }
    if len(partialFiles) > 0 {
        partials, err := template.New("partials").Funcs(funcMap).ParseGlob(partialsGlob)
        if err != nil {
            return nil, fmt.Errorf("failed to parse partial templates: %w", err)
        }
        cache.partials = partials
    }

    // Load other templates (e.g., base.html, header.html) and apply funcMap globally
    err = filepath.Walk(layoutsDir, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".html") {
            return err
        }

        // Apply the funcMap to each template
        tmpl, err := template.New(filepath.Base(path)).Funcs(funcMap).ParseFiles(path)
        if err != nil {
            log.Printf("Skipping template %s due to parsing error: %v", path, err)
            return nil
        }

        // Register the template in the cache
        cache.templates[filepath.Base(path)] = tmpl
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
		log.Printf("Loading partial: %s", partialPath) // Debug log
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
		return "", fmt.Errorf("failed to convert markdown to HTML: %w", err)
	}
	return buf.String(), nil
}

func writeHTMLFile(filePath string, frontMatter FrontMatter, content string, themeDir string, config Config) error {
	// Create template data with the content and front matter
	data := TemplateData{
		Site:    config,
		Page:    frontMatter,
		Content: content,
	}

	// Load the base template and render it
	tmpl, ok := templateCache.templates["base.html"]
	if !ok {
		return fmt.Errorf("base template not found")
	}

	// Execute the template and write it to the output file
	outputFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	if err := tmpl.Execute(outputFile, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

func renderTaxonomies(cache *TemplateCache, taxonomies map[string][]string, postsByTerm map[string]map[string][]Post, publicDir string) {
	// Rendering taxonomies (e.g., tags, categories)
	taxonomyTemplate := cache.templates["taxonomy.html"]
	if taxonomyTemplate == nil {
		log.Println("Warning: Taxonomy template not found.")
		return
	}

	// Generate and render taxonomy pages
	for termType, terms := range taxonomies {
		for _, term := range terms {
			// Create a taxonomy data structure
			data := map[string]interface{}{
				"TermType": termType,
				"Term":     term,
				"Posts":    postsByTerm[termType][term],
			}

			// Render the taxonomy page for each term
			outputPath := filepath.Join(publicDir, termType, term+".html")
			outputFile, err := os.Create(outputPath)
			if err != nil {
				log.Printf("Failed to create taxonomy page %s: %v", outputPath, err)
				continue
			}
			defer outputFile.Close()

			if err := taxonomyTemplate.Execute(outputFile, data); err != nil {
				log.Printf("Failed to render taxonomy page %s: %v", outputPath, err)
			}
		}
	}
}

func copyStaticFiles(themeDir, publicDir string) {
	// Copy static files (e.g., images, CSS, JavaScript)
	staticDir := filepath.Join(themeDir, "static")
	err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Copy file to public directory
		relativePath := strings.TrimPrefix(path, staticDir)
		destPath := filepath.Join(publicDir, relativePath)
		destDir := filepath.Dir(destPath)

		// Create destination directory if it does not exist
		if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
			return err
		}

		// Copy the file
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, sourceFile); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("Failed to copy static files: %v", err)
	}
}
