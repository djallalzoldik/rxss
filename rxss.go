package main

import (
    "bufio"
    "bytes"
    "encoding/base64"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "unicode/utf8"
)

const (
    maxWorkers = 10
    defaultPayload = `'"%00><h1>akira</h1>`
)

func main() {
    methodPtr := flag.String("method", "GET", "HTTP method to use (GET, POST, PATCH, etc.)")
    keepPtr := flag.Bool("keep", false, "Keep query parameters in the URL when sending POST requests")
    headersPtr := flag.String("H", "", "Custom headers (comma-separated) to include in the request")
    outputPtr := flag.String("o", "", "Custom output file")
    payloadPtr := flag.String("py", defaultPayload, "Custom payload")
    contentTypePtr := flag.String("type", "form", "Content type for POST requests (form, json, xml)")
    flag.Parse()

    if *methodPtr != "GET" && *methodPtr != "POST" && *methodPtr != "PATCH" {
        fmt.Println("Invalid HTTP method. Supported methods: GET, POST, PATCH.")
        return
    }

    var outputFile *os.File
    if *outputPtr != "" {
        var err error
        outputFile, err = os.Create(*outputPtr)
        if err != nil {
            fmt.Printf("Error creating output file: %v\n", err)
            return
        }
        defer outputFile.Close()
    }

    scanner := bufio.NewScanner(os.Stdin)

    urlCh := make(chan string, maxWorkers)
    var wg sync.WaitGroup

    for i := 0; i < maxWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for u := range urlCh {
                processURL(u, outputFile, *methodPtr, *keepPtr, *headersPtr, *payloadPtr, *contentTypePtr)
            }
        }()
    }

    for scanner.Scan() {
        urlCh <- scanner.Text()
    }
    close(urlCh)

    wg.Wait()

    if err := scanner.Err(); err != nil {
        fmt.Fprintln(os.Stderr, "reading standard input:", err)
    }
}

func processURL(u string, outputFile *os.File, method string, keep bool, headers string, customPayload string, contentType string) {
    parsedURL, err := url.Parse(u)
    if err != nil {
        fmt.Printf("Error parsing URL %s: %v\n", u, err)
        return
    }

    queryValues := parsedURL.Query()
    var queryKeys []string
    for key := range queryValues {
        queryKeys = append(queryKeys, key)
    }

    if len(queryKeys) == 0 {
        fmt.Printf("No query parameters found in URL %s\n", u)
        return
    }

    client := &http.Client{}

    var requestBody io.Reader = nil
    var contentTypeHeader string

    if method == "POST" || method == "PATCH" {
        if !keep {
            // Remove query parameters from the URL
            parsedURL.RawQuery = ""
        }

        if contentType == "json" {
            // Convert to JSON format
            jsonParams := make(map[string]string)
            for _, key := range queryKeys {
                value := queryValues.Get(key)
                jsonParams[key] = value
            }
            jsonPayload, _ := json.Marshal(jsonParams)
            requestBody = bytes.NewReader(jsonPayload)
            contentTypeHeader = "application/json"
        } else if contentType == "xml" {
            // Convert to XML format
            // Implement XML conversion logic here if needed
            fmt.Println("XML conversion is not implemented.")
            return
        } else {
            // Default to application/x-www-form-urlencoded
            bodyParams := url.Values{}
            for _, key := range queryKeys {
                value := queryValues.Get(key)
                bodyParams.Add(key, value)
            }
            requestBody = strings.NewReader(bodyParams.Encode())
            contentTypeHeader = "application/x-www-form-urlencoded"
        }
    }

    req, err := http.NewRequest(method, parsedURL.String(), requestBody)
    if err != nil {
        fmt.Printf("Error creating %s request for URL %s: %v\n", method, parsedURL.String(), err)
        return
    }

    // Add custom headers
    if headers != "" {
        headerList := strings.Split(headers, ",")
        for _, header := range headerList {
            parts := strings.SplitN(header, ":", 2)
            if len(parts) == 2 {
                req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
            }
        }
    }

    // Set content type header
    if contentTypeHeader != "" {
        req.Header.Set("Content-Type", contentTypeHeader)
    }

    resp, err := client.Do(req)
    if err != nil {
        fmt.Printf("Error sending %s request to URL %s: %v\n", method, parsedURL.String(), err)
        return
    }
    defer resp.Body.Close()

    buf := bytes.NewBuffer(nil)
    if _, err := io.Copy(buf, resp.Body); err != nil {
        fmt.Printf("Error reading response body from URL %s: %v\n", parsedURL.String(), err)
        return
    }

    for _, key := range queryKeys {
        value := queryValues.Get(key)

        // Decode the value to ensure consistent comparison
        decodedValue, err := url.QueryUnescape(value)
        if err != nil {
            fmt.Printf("Error decoding URL-encoded value '%s': %v\n", value, err)
            continue
        }

        if strings.Contains(buf.String(), decodedValue) {
            injectedValue := decodedValue + customPayload
            queryValues.Set(key, injectedValue)
            parsedURL.RawQuery = queryValues.Encode()
            injectedURL := parsedURL.String()

            result := fmt.Sprintf("Query parameter '%s' with value '%s' reflected in response body of %s, replaced with payload %q\n", key, decodedValue, parsedURL, customPayload)
            fmt.Print(result)

            if outputFile != nil {
                if _, err := outputFile.WriteString(injectedURL + "\n"); err != nil {
                    fmt.Printf("Error writing to file: %v\n", err)
                }
            }
        } else {
            result := fmt.Sprintf("Query parameter '%s' with value '%s' not found in response body of %s\n", key, decodedValue, parsedURL)
            fmt.Print(result)
        }
    }
}

func isBase64Encoded(s string) bool {
    for len(s)%4 != 0 {
        s += "="
    }
    for _, c := range s {
        if !strings.Contains("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", string(c)) {
            return false
        }
    }
    decoded, err := base64.StdEncoding.DecodeString(s)
    if err == nil && utf8.Valid(decoded) {
        return true
    }
    return false
}

func isURLEncoded(s string) bool {
    _, err := url.QueryUnescape(s)
    return err == nil
}
