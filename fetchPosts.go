package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

type FrontMatter struct {
	Title       string `yaml:"title" toml:"title"`
	Description string `yaml:"description" toml:"description"`
	Date        string `yaml:"date" toml:"date"`
}

func main() {
	postsDir := "./posts/"
	publicDir := "./public/"

	if err := os.MkdirAll(publicDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create public directory: %v", err)
	}

	files, err := ioutil.ReadDir(postsDir)
	if err != nil {
		log.Fatalf("Failed to read posts directory: %v", err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".md" {
			processMarkdownFile(filepath.Join(postsDir, file.Name()), publicDir)
		}
	}
}

func processMarkdownFile(filePath, outputDir string) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read file %s: %v", filePath, err)
		return
	}

	frontMatter, markdownContent := extractFrontMatter(content)
	htmlContent, err := convertMarkdownToHTML(markdownContent)
	if err != nil {
		log.Printf("Failed to convert Markdown for %s: %v", filePath, err)
		return
	}

	outputFileName := filepath.Base(filePath[:len(filePath)-len(filepath.Ext(filePath))]) + ".html"
	outputPath := filepath.Join(outputDir, outputFileName)

	writeHTMLFile(outputPath, frontMatter, htmlContent)
}

func extractFrontMatter(content []byte) (FrontMatter, []byte) {
	var fm FrontMatter
	contentStr := string(content)
	
	if strings.HasPrefix(contentStr, "---") || strings.HasPrefix(contentStr, "+++") {
		parts := strings.SplitN(contentStr, "\n---\n", 2)
		if len(parts) < 2 {
			parts = strings.SplitN(contentStr, "\n+++\n", 2)
		}

		if len(parts) == 2 {
			meta := parts[0]
			body := parts[1]

			if strings.HasPrefix(meta, "---") {
				if err := yaml.Unmarshal([]byte(meta), &fm); err != nil {
					log.Printf("Failed to parse YAML front matter: %v", err)
				}
			} else if strings.HasPrefix(meta, "+++") {
				if err := toml.Unmarshal([]byte(meta), &fm); err != nil {
					log.Printf("Failed to parse TOML front matter: %v", err)
				}
			}
			return fm, []byte(body)
		}
	}
	return fm, content
}

func convertMarkdownToHTML(content []byte) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)

	var buf strings.Builder
	if err := md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeHTMLFile(outputPath string, fm FrontMatter, htmlContent string) {
	file, err := os.Create(outputPath)
	if err != nil {
		log.Printf("Failed to create HTML file: %v", err)
		return
	}
	defer file.Close()

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
</html>`, fm.Title, fm.Description, htmlContent))

	if err != nil {
		log.Printf("Failed to write to HTML file: %v", err)
	}
}
