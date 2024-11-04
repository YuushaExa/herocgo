package main

import (
    "encoding/json"
    "fmt"
    "html/template"
    "io/ioutil"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/pelletier/go-toml/v2" // Use this package for TOML
    "github.com/yuin/goldmark"
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
    Folder  string
}

func main() {
    startTime := time.Now()
    postsDirPath := "./posts" // Directory containing JSON and MD files
    outputDir := "./public"    // Output directory for generated HTML files
    archetypePath := "./archetypes/post.md" // Path to the archetype file

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
            title := strings.TrimSuffix(info.Name(), ".md")
            folder := filepath.Dir(path)
            date := time.Now().Format(time.RFC3339)
            content := string(data) // Use the raw content of the Markdown file
            allPosts = append(allPosts, Post{Title: title, Content: content, Date: date, Folder: folder})
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

    titleCount := make(map[string]bool)

    md := goldmark.New()

    // Read the archetype file
    archetypeData, err := ioutil.ReadFile(archetypePath)
    if err != nil {
        fmt.Println("Error reading archetype file:", err)
        return
    }

    for _, post := range allPosts {
        baseFileName := strings.ToLower(strings.ReplaceAll(post.Title, " ", "-"))
           fileName := fmt.Sprintf("%s.html", baseFileName)
        count := 1

        // Check for duplicates and modify the file name if necessary
        for titleCount[fileName] {
            fileName = fmt.Sprintf("%s-%d.html", baseFileName, count)
            count++
        }
        titleCount[fileName] = true

        folderPath := filepath.Join(outputDir, post.Folder)
        os.MkdirAll(folderPath, os.ModePerm)

        filePath := filepath.Join(folderPath, fileName)

        // Convert Markdown to HTML
        var buf strings.Builder
        if err := md.Convert([]byte(post.Content), &buf); err != nil {
            fmt.Println("Error converting Markdown to HTML:", err)
            return
        }

        // Create a data structure for the template
        data := struct {
            Title   string
            Content string
            BaseURL string
            Author  string
            Date    string
        }{
            Title:   post.Title,
            Content: buf.String(),
            BaseURL: config.BaseURL,
            Author:  config.Params.Author,
            Date:    post.Date,
        }

        // Create the output HTML file using the archetype as a template
        tmpl, err := template.New("post").Parse(string(archetypeData))
        if err != nil {
            fmt.Println("Error parsing template:", err)
            return
        }

        // Create the output file
        file, err := os.Create(filePath)
        if err != nil {
            fmt.Println("Error creating file:", err)
            return
        }
        defer file.Close()

        // Execute the template and write to file
        if err := tmpl.Execute(file, data); err != nil {
            fmt.Println("Error executing template:", err)
            return
        }

        // Log the relative URL
        relativeUrl := filepath.Join(post.Folder, fileName)
        fmt.Println("Created post:", relativeUrl)
    }

    // After processing all posts, log the statistics
    fmt.Println("--- Build Statistics ---")
    fmt.Printf("Total Pages: %d\n", totalPages)
    fmt.Printf("Non-page Files: %d\n", nonPageFiles)
    fmt.Printf("Static Files: %d\n", staticFiles)
    fmt.Printf("Total Build Time: %v\n", time.Since(startTime))
}
