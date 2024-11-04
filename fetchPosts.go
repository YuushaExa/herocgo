package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"
    "strings"
    "time"
)

type Post struct {
    Title   string `json:"title"`
    Content string `json:"content"`
    Date    string `json:"date"`
    Folder  string `json:"folder"`
}

func main() {
    startTime := time.Now()
    postsDirPath := "./posts" // Directory containing JSON and MD files
    outputDir := "./public"    // Output directory for generated HTML files

    allPosts := []Post{}
    totalPages := 0
    nonPageFiles := 0
    staticFiles := 0

    err := filepath.Walk(postsDirPath, func(path string, info os.FileInfo, err error) error {
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

        htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
</head>
<body>
    <h1>%s</h1>
    <div>%s</div> 
</body>
</html>
`, post.Title, post.Title, post.Content)

        err := ioutil.WriteFile(filePath, []byte(htmlContent), 0644)
        if err != nil {
            fmt.Println("Error writing file:", err)
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
