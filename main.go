package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
}

func scrapeWebsite(ctx context.Context, url string) (string, error) {
	fmt.Println("Scraping website, please wait...")
	var htmlContent string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		return "", fmt.Errorf("failed to scrape website: %w", err)
	}
	return htmlContent, nil
}

func extractBodyContent(htmlContent string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML content: %w", err)
	}
	bodyContent, _ := doc.Find("body").Html()
	return bodyContent, nil
}

func cleanBodyContent(bodyContent string) string {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(bodyContent))
	doc.Find("script").Remove()
	doc.Find("style").Remove()
	cleanedContent := doc.Text()
	return cleanedContent
}

func splitDOMContent(domContent string, maxLength int) []string {
	var chunks []string
	for i := 0; i < len(domContent); i += maxLength {
		end := i + maxLength
		if end > len(domContent) {
			end = len(domContent)
		}
		chunks = append(chunks, domContent[i:end])
	}
	return chunks
}

func parseWithGenAI(domChunks []string, parseDescription string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY not set")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("failed to create GenAI client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")

	var results []string
	maxChunks := 16
	chunkCount := min(len(domChunks), maxChunks)

	for i := 0; i < chunkCount; i++ {
		fmt.Printf("Processing chunk %d of %d...\n", i+1, chunkCount)
		resp, err := model.GenerateContent(
			ctx,
			genai.Text(parseDescription),
			genai.Text(domChunks[i]),
		)
		if err != nil {
			return "", fmt.Errorf("failed to generate content for chunk %d: %w", i+1, err)
		}

		var resultBuilder strings.Builder
		for _, cand := range resp.Candidates {
			if cand.Content != nil {
				for _, part := range cand.Content.Parts {
					if str, ok := part.(fmt.Stringer); ok {
						resultBuilder.WriteString(str.String())
					} else {
						resultBuilder.WriteString(fmt.Sprint(part)) // Use fmt.Sprint if Stringer isn't available
					}
					resultBuilder.WriteString("\n")
				}
			}
		}
		results = append(results, resultBuilder.String())
	}

	return strings.Join(results, "\n"), nil
}

func printFormatted(text string, lineWidth int) string {
	var result strings.Builder
	for len(text) > 0 {
		if len(text) > lineWidth {
			result.WriteString(text[:lineWidth] + "\n")
			text = text[lineWidth:]
		} else {
			result.WriteString(text + "\n")
			text = ""
		}
	}
	return result.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChan
		fmt.Println("\nReceived shutdown signal, exiting...")
		os.Exit(0)
	}()

	for {
		fmt.Print("Enter the website URL you want to scrape (or type 'exit' to quit): ")
		url, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Error reading URL:", err)
		}
		url = strings.TrimSpace(url)

		if strings.ToLower(url) == "exit" {
			fmt.Println("Exiting application.")
			return
		}

		ctx, cancel := chromedp.NewContext(context.Background())
		defer cancel()

		htmlContent, err := scrapeWebsite(ctx, url)
		if err != nil {
			fmt.Println("Error scraping website:", err)
			continue
		}

		bodyContent, err := extractBodyContent(htmlContent)
		if err != nil {
			fmt.Println("Error extracting body content:", err)
			continue
		}

		cleanedContent := cleanBodyContent(bodyContent)

		domChunks := splitDOMContent(cleanedContent, 6000)

		fmt.Print("Describe what you want to parse from the website: ")
		parseDescription, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading parse description:", err)
			continue
		}
		parseDescription = strings.TrimSpace(parseDescription)

		fmt.Println("Processing your request, please wait...")
		parsedResult, err := parseWithGenAI(domChunks, parseDescription)
		if err != nil {
			fmt.Println("Error parsing content:", err)
			continue
		}

		fmt.Println("Parsed Result:\n", printFormatted(parsedResult, 80))
	}
}
