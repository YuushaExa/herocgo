func main() {
    startTime := time.Now()
    postsDirPath := "./posts" // Directory containing JSON and MD files
    outputDir := "./public"    // Output directory for generated HTML files
    archetypePath := "./archetypes/post.md" // Path to the archetype file

    // Create output directory
    os.MkdirAll(outputDir, os.ModePerm)

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

    archetypeData, err := ioutil.ReadFile(archetypePath)
    if err != nil {
        fmt.Println("Error reading archetype file:", err)
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

    // After processing all posts, log the statistics
    fmt.Println("--- Build Statistics ---")
    fmt.Printf("Total Pages: %d\n", totalPages)
    fmt.Printf("Non-page Files: %d\n", nonPageFiles)
    fmt.Printf("Static Files: %d\n", staticFiles)
    fmt.Printf("Total Build Time: %v\n", time.Since(startTime))
}

