package main

import (
    "bytes"
    "encoding/json"
    "flag"
    "fmt"
    "html/template"
    "io/ioutil"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/pelletier/go-toml/v2" // Use this package for TOML
    "github.com/yuin/goldmark"
    "gopkg.in/yaml.v3" // For parsing front matter
)

type Config struct {
    BaseURL      string `toml:"baseURL"`
    Title        string `toml:"title"`
    Theme        string `toml:"theme"`
    LanguageCode string `toml:"languageCode"`
    Params       Params `toml:"params"`
}

type Params struct {
    Author      string `toml:"author"`
    Description string `toml:"description"`
}

type Post struct {
    Title   string
    Content string
    Date    string
    Author  string
}

func parseFrontMatter(data []byte) (Post, string, error) {
    // Split the front matter and content
    parts := bytes.SplitN(data, []byte("---"), 3)
    if len(parts) < 3 {
        return Post{}, "", fmt.Errorf("invalid front matter format")
    }

    // Parse front matter
    var post Post
    if err := yaml.Unmarshal(parts[1], &post); err != nil {
        return Post{}, "", err
    }

    // Get the content
    content := string(parts[2])
    return post, content, nil
}

func createPost(title, archetypePath, outputDir, author string) error {
    // Read the archetype file
    archetypeData, err := ioutil.ReadFile(archetypePath)
    if err != nil {
        return fmt.Errorf("error reading archetype file: %w", err)
    }

    // Prepare the data for the template
    data := struct {
        Title   string
        Date    string
        Author  string
        Content string
    }{
        Title:   title,
        Date:    time.Now().Format(time.RFC3339),
        Author:  author,
        Content: "", // You can set default content or leave it empty
    }

    // Create the output file name
    baseFileName := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
    fileName := fmt.Sprintf("%s.md", baseFileName)
    filePath := filepath.Join(outputDir, fileName)

    // Create the output file
    file, err := os.Create(filePath)
    if err != nil {
        return fmt.Errorf("error creating file: %w", err)
    }
    defer file.Close()

    // Create a template and execute it
    tmpl, err := template.New("archetype").Parse(string(archetypeData))
    if err != nil {
        return fmt.Errorf("error parsing template: %w", err)
    }

    if err := tmpl.Execute(file, data); err != nil {
        return fmt.Errorf("error executing template: %w", err)
    }

    fmt.Println("Created new post:", filePath)
    return nil
}

func main() {
    startTime := time.Now()
    postsDirPath := "./posts" // Directory containing JSON and MD files
    outputDir := "./public"    // Output directory for generated HTML files
    archetypePath := "./archetypes/post.md" // Path to the archetype file

    // Command-line flags
    createPostFlag := flag.String("new", "", "Create a new post with the given title")
    flag.Parse()

    // Load configuration
    config := Config{}
    configData, err := ioutil.ReadFile("config.toml")
    if err != nil {
        fmt.Println("Error reading config.toml:", err)
        return
    }

    // Decode the TOML file
    if err := toml.Unmarshal(configData, &config); err != nil {
        fmt.Println("Error decoding config.toml:", err)
        return
    }

    // If the -new flag is set, create a new post
    if *createPostFlag != "" {
        if err := createPost(*createPostFlag, archetypePath, postsDirPath, config.Params.Author); err != nil {
            fmt.Println("Error creating post:", err)
            return
        }
        return
    }

    allPosts := []Post{}
    totalPages := 0
    nonPageFiles := 0
    staticFiles := 0

      err = filepath.Walk(postsDirPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }

        if info.IsDir() {
            return nil
        }

        switch {
        case strings.HasSuffix(info.Name(), ".json"):
            // Read and parse JSON files
            data, err := ioutil.ReadFile(path)
            if err != nil {
                return err
            }
            var posts []Post
            err = json.Unmarshal(data, &posts)
            if err != nil {
                return err
            }
            folder := filepath.Dir(path)
            for i := range posts {
                posts[i].Folder = folder
                allPosts = append(allPosts, posts[i])
            }
            totalPages += len(posts)
            nonPageFiles++

        case strings.HasSuffix(info.Name(), ".md"):
            // Read Markdown files
            data, err := ioutil.ReadFile(path)
            if err != nil {
                return err
            }

            // Parse front matter and content
            post, content, err := parseFrontMatter(data)
            if err != nil {
                return err
            }

            // Convert Markdown content to HTML
            var buf strings.Builder
            md := goldmark.New()
            if err := md.Convert([]byte(content), &buf); err != nil {
                return err
            }

            // Create a data structure for the template
            dataForTemplate := struct {
                Title   string
                Content string
                BaseURL string
                Author  string
                Date    string
            }{
                Title:   post.Title,
                Content: buf.String(),
                BaseURL: config.BaseURL,
                Author:  post.Author,
                Date:    post.Date,
            }

            // Create the output HTML file using the archetype as a template
            tmpl, err := template.New("post").Parse(string(archetypeData))
            if err != nil {
                return fmt.Errorf("error parsing template: %w", err)
            }

            // Create the output file
            outputFileName := strings.ToLower(strings.ReplaceAll(post.Title, " ", "-")) + ".html"
            outputFilePath := filepath.Join(outputDir, outputFileName)

            file, err := os.Create(outputFilePath)
            if err != nil {
                return fmt.Errorf("error creating file: %w", err)
            }
            defer file.Close()

            // Execute the template and write to file
            if err := tmpl.Execute(file, dataForTemplate); err != nil {
                return fmt.Errorf("error executing template: %w", err)
            }

            // Log the relative URL
            relativeUrl := filepath.Join(post.Folder, outputFileName)
            fmt.Println("Created post:", relativeUrl)
            totalPages++

        default:
            staticFiles++
        }
        return nil
    })

    if err != nil {
        fmt.Println("Error:", err)
        return
    }

    // Create output directory
    os.MkdirAll(outputDir, os.ModePerm)

    // After processing all posts, log the statistics
    fmt.Println("--- Build Statistics ---")
    fmt.Printf("Total Pages: %d\n", totalPages)
    fmt.Printf("Non-page Files: %d\n", nonPageFiles)
    fmt.Printf("Static Files: %d\n", staticFiles)
    fmt.Printf("Total Build Time: %v\n", time.Since(startTime))
}
