// crawler.go
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Concurrency      int  `json:"concurrency"`
	Delay            int  `json:"delay"` // in milliseconds (changed from time.Duration to int)
	MaxDepth         int  `json:"maxDepth"`
	StayInDomain     bool `json:"stayInDomain"`
	DownloadFullPage bool `json:"downloadFullPage"`
}

// Domain represents a website to crawl
type Domain struct {
	ID         int       `json:"id"`
	URL        string    `json:"url"`
	LastCrawl  time.Time `json:"lastCrawl"`
	Status     string    `json:"status"` // pending, active, completed, failed
	PagesFound int       `json:"pagesFound"`
}

// CrawlResult represents metadata extracted from a webpage
type CrawlResult struct {
	URL         string
	Title       string
	Description string
	Content     string
	ImageURLs   []string
	FullHTML    string // Store the full HTML content
}

// Crawler manages the web crawling process
type Crawler struct {
	db              *sql.DB
	config          Config
	domains         []Domain
	visited         map[string]bool
	queue           []string
	mu              sync.Mutex
	wg              sync.WaitGroup
	semaphore       chan struct{}
	isRunning       bool
	stopSignal      chan struct{}
	activityLog     []string
	activityLogSize int
}

// NewCrawler creates a new crawler instance
func NewCrawler(db *sql.DB) *Crawler {
	// Default configuration
	config := Config{
		Concurrency: 3,

		MaxDepth:         3,
		StayInDomain:     true,
		DownloadFullPage: false,
	}

	return &Crawler{
		db:              db,
		config:          config,
		domains:         []Domain{},
		visited:         make(map[string]bool),
		queue:           []string{},
		semaphore:       make(chan struct{}, config.Concurrency),
		isRunning:       false,
		stopSignal:      make(chan struct{}),
		activityLog:     []string{},
		activityLogSize: 100,
	}
}

// Start begins the crawling process
func (c *Crawler) Start() {
	if c.isRunning {
		log.Println("Crawler is already running")
		return
	}

	c.mu.Lock()
	c.isRunning = true
	c.visited = make(map[string]bool)
	c.queue = []string{}
	c.stopSignal = make(chan struct{})
	c.mu.Unlock()

	// Load domains from database
	err := c.loadDomains()
	if err != nil {
		log.Printf("Error loading domains: %v", err)
		return
	}

	// Set semaphore size based on current config
	c.semaphore = make(chan struct{}, c.config.Concurrency)

	// Ensure full_pages directory exists if we're downloading full pages
	if c.config.DownloadFullPage {
		if err := os.MkdirAll("full_pages", 0755); err != nil {
			log.Printf("Error creating full_pages directory: %v", err)
		}
	}

	c.logActivity("Crawler started")

	go func() {
		// Add all pending domains to the queue
		for i := range c.domains {
			if c.domains[i].Status == "pending" {
				c.updateDomainStatus(c.domains[i].ID, "active")
				c.addDomainToQueue(c.domains[i].URL)
			}
		}

		for {
			select {
			case <-c.stopSignal:
				// Wait for all goroutines to finish
				c.wg.Wait()
				c.mu.Lock()
				c.isRunning = false
				c.mu.Unlock()
				c.logActivity("Crawler stopped")
				log.Println("Crawler stopped")
				return
			default:
				url, depth, ok := c.NextURL()
				if !ok {
					// No more URLs to crawl
					if len(c.semaphore) == 0 {
						// All workers are done
						c.mu.Lock()
						c.isRunning = false
						c.mu.Unlock()
						c.logActivity("Crawling complete")
						log.Println("Crawling complete")
						return
					}
					time.Sleep(500 * time.Millisecond)
					continue
				}

				c.semaphore <- struct{}{} // Acquire semaphore
				c.wg.Add(1)
				go func(url string, depth int) {
					defer c.wg.Done()
					defer func() { <-c.semaphore }() // Release semaphore

					result, urls, err := c.CrawlPage(url)
					if err != nil {
						c.logActivity(fmt.Sprintf("Error crawling %s: %v", url, err))
						log.Printf("Error crawling %s: %v", url, err)
						return
					}

					// Only process found URLs if we haven't reached max depth
					if depth < c.config.MaxDepth {
						for _, u := range urls {
							c.AddURL(u, depth+1)
						}
					}

					// Save the result to database
					err = c.SaveResult(result)
					if err != nil {
						c.logActivity(fmt.Sprintf("Error saving result for %s: %v", url, err))
						log.Printf("Error saving result for %s: %v", url, err)
					}

					// Respect crawl delay
					time.Sleep(time.Duration(c.config.Delay) * time.Millisecond)

				}(url, depth)
			}
		}
	}()
}

// Stop halts the crawler process
func (c *Crawler) Stop() {
	if !c.isRunning {
		log.Println("Crawler is not running")
		return
	}

	close(c.stopSignal)
}

// logActivity adds a message to the activity log
func (c *Crawler) logActivity(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add timestamp to message
	logEntry := time.Now().Format("15:04:05") + " - " + message

	// Add to the beginning of the log (newest first)
	c.activityLog = append([]string{logEntry}, c.activityLog...)

	// Trim log if it exceeds the maximum size
	if len(c.activityLog) > c.activityLogSize {
		c.activityLog = c.activityLog[:c.activityLogSize]
	}
}

// loadDomains loads domains from the database
func (c *Crawler) loadDomains() error {
	rows, err := c.db.Query(`
		SELECT id, url, last_crawl, status, pages_found 
		FROM domains
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var d Domain
		var lastCrawl sql.NullTime
		err := rows.Scan(&d.ID, &d.URL, &lastCrawl, &d.Status, &d.PagesFound)
		if err != nil {
			return err
		}
		if lastCrawl.Valid {
			d.LastCrawl = lastCrawl.Time
		}
		domains = append(domains, d)
	}

	c.mu.Lock()
	c.domains = domains
	c.mu.Unlock()

	return nil
}

// updateDomainStatus updates a domain's status
func (c *Crawler) updateDomainStatus(id int, status string) error {
	_, err := c.db.Exec(`
		UPDATE domains 
		SET status = ?, last_crawl = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, id)
	if err != nil {
		return err
	}

	// Update local copy
	c.mu.Lock()
	for i := range c.domains {
		if c.domains[i].ID == id {
			c.domains[i].Status = status
			c.domains[i].LastCrawl = time.Now()
			break
		}
	}
	c.mu.Unlock()

	return nil
}

// addDomainToQueue adds a domain's URL to the crawl queue
func (c *Crawler) addDomainToQueue(domainURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.queue = append(c.queue, fmt.Sprintf("%s|0", domainURL))
}

// NextURL gets the next URL to crawl
func (c *Crawler) NextURL() (string, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.queue) == 0 {
		return "", 0, false
	}

	parts := strings.Split(c.queue[0], "|")
	c.queue = c.queue[1:]

	url := parts[0]
	depth := 0
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &depth)
	}

	return url, depth, true
}

// AddURL adds a URL to the crawl queue if it hasn't been visited
func (c *Crawler) AddURL(rawURL string, depth int) {
	// Normalize the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return
	}

	// Ensure URL has scheme and host
	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "https"
	}

	// Skip non-HTTP/HTTPS URLs
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return
	}

	// Check if URL is within allowed domain if stayInDomain is true
	if c.config.StayInDomain {
		// Get the domain of the URL
		urlDomain := parsedURL.Host

		// Check if this domain is in our list of domains to crawl
		found := false
		c.mu.Lock()
		for _, domain := range c.domains {
			domainURL, err := url.Parse(domain.URL)
			if err != nil {
				continue
			}
			if domainURL.Host == urlDomain {
				found = true
				break
			}
		}
		c.mu.Unlock()

		if !found {
			return
		}
	}

	// Remove fragment
	parsedURL.Fragment = ""

	// Convert to string for storage
	normalizedURL := parsedURL.String()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Skip if already visited or already in queue
	if c.visited[normalizedURL] {
		return
	}
	c.visited[normalizedURL] = true

	// Add to crawl queue with depth info
	c.queue = append(c.queue, fmt.Sprintf("%s|%d", normalizedURL, depth))
}

// CrawlPage fetches and processes a web page
func (c *Crawler) CrawlPage(pageURL string) (*CrawlResult, []string, error) {
	c.logActivity("Crawling: " + pageURL)
	log.Printf("Crawling: %s", pageURL)

	// Fetch the page
	resp, err := http.Get(pageURL)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// Store the full HTML if configured
	fullHTML := ""
	if c.config.DownloadFullPage {
		fullHTML = string(body)
	}

	// Parse the HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}

	// Create result
	result := &CrawlResult{
		URL:         pageURL,
		Title:       doc.Find("title").Text(),
		Description: "",
		Content:     "",
		ImageURLs:   []string{},
		FullHTML:    fullHTML,
	}

	// Extract description
	doc.Find("meta[name='description']").Each(func(i int, s *goquery.Selection) {
		if content, exists := s.Attr("content"); exists {
			result.Description = content
		}
	})

	// Extract main content
	mainContent := []string{}
	doc.Find("p, h1, h2, h3, h4, h5, h6").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" {
			mainContent = append(mainContent, text)
		}
	})
	result.Content = strings.Join(mainContent, " ")

	// Extract image URLs
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists {
			// Resolve relative URLs
			imageURL, err := url.Parse(src)
			if err != nil {
				return
			}

			baseURL, err := url.Parse(pageURL)
			if err != nil {
				return
			}

			resolvedURL := baseURL.ResolveReference(imageURL)
			result.ImageURLs = append(result.ImageURLs, resolvedURL.String())
		}
	})

	// Extract links for further crawling
	var links []string
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists {
			// Resolve relative URLs
			linkURL, err := url.Parse(href)
			if err != nil {
				return
			}

			baseURL, err := url.Parse(pageURL)
			if err != nil {
				return
			}

			resolvedURL := baseURL.ResolveReference(linkURL)
			links = append(links, resolvedURL.String())
		}
	})

	// Update the domain's page count
	c.incrementDomainPageCount(pageURL)
	c.logActivity(fmt.Sprintf("Crawled successfully: %s (found %d links, %d images)",
		pageURL, len(links), len(result.ImageURLs)))

	return result, links, nil
}

// incrementDomainPageCount updates the page count for a domain
func (c *Crawler) incrementDomainPageCount(pageURL string) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.domains {
		domainURL, err := url.Parse(c.domains[i].URL)
		if err != nil {
			continue
		}

		if domainURL.Host == parsedURL.Host {
			c.domains[i].PagesFound++
			_, err = c.db.Exec(`
				UPDATE domains 
				SET pages_found = ? 
				WHERE id = ?
			`, c.domains[i].PagesFound, c.domains[i].ID)
			if err != nil {
				log.Printf("Error updating domain page count: %v", err)
			}
			break
		}
	}
}

// SaveResult stores the crawl result in the database
func (c *Crawler) SaveResult(result *CrawlResult) error {
	// Begin transaction
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if page already exists
	var existingID int
	err = tx.QueryRow("SELECT id FROM pages WHERE url = ?", result.URL).Scan(&existingID)
	if err == nil {
		// Page exists, update it
		_, err = tx.Exec(`
			UPDATE pages 
			SET title = ?, description = ?, content = ?
			WHERE id = ?
		`, result.Title, result.Description, result.Content, existingID)
		if err != nil {
			return err
		}
	} else if err == sql.ErrNoRows {
		// Page doesn't exist, insert it
		_, err = tx.Exec(`
			INSERT INTO pages (title, url, description, content) 
			VALUES (?, ?, ?, ?)
		`, result.Title, result.URL, result.Description, result.Content)
		if err != nil {
			return err
		}
	} else {
		// Some other error
		return err
	}

	// Get the page ID
	var pageID int
	err = tx.QueryRow(`SELECT id FROM pages WHERE url = ?`, result.URL).Scan(&pageID)
	if err != nil {
		return err
	}

	// Save full HTML content if needed
	if c.config.DownloadFullPage && result.FullHTML != "" {
		// Check if the page already exists in the full_pages table
		var fullPageID int
		err = tx.QueryRow("SELECT id FROM full_pages WHERE page_id = ?", pageID).Scan(&fullPageID)
		if err == nil {
			// Update existing record
			_, err = tx.Exec(`
				UPDATE full_pages
				SET html_content = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, result.FullHTML, fullPageID)
		} else if err == sql.ErrNoRows {
			// Insert new record
			_, err = tx.Exec(`
				INSERT INTO full_pages (page_id, html_content)
				VALUES (?, ?)
			`, pageID, result.FullHTML)
		} else {
			// Some other error
			return err
		}

		if err != nil {
			log.Printf("Error saving full HTML: %v", err)
		}
	}

	// Insert images
	for _, imageURL := range result.ImageURLs {
		// Extract a title/alt text from the last part of the URL
		urlParts := strings.Split(imageURL, "/")
		imgName := urlParts[len(urlParts)-1]
		imgName = strings.Split(imgName, "?")[0] // Remove query params

		// Check if image already exists
		var existingImageID int
		err = tx.QueryRow("SELECT id FROM images WHERE image_url = ?", imageURL).Scan(&existingImageID)
		if err == nil {
			// Image exists, update the relationship
			_, err = tx.Exec(`
				UPDATE images
				SET title = ?, url = ?, description = ?, alt_text = ?
				WHERE id = ?
			`, imgName, result.URL, fmt.Sprintf("Image from %s", result.Title), imgName, existingImageID)
		} else if err == sql.ErrNoRows {
			// Image doesn't exist, insert it
			_, err = tx.Exec(`
				INSERT OR IGNORE INTO images (title, url, image_url, description, alt_text)
				VALUES (?, ?, ?, ?, ?)
			`, imgName, result.URL, imageURL, fmt.Sprintf("Image from %s", result.Title), imgName)
		}

		if err != nil {
			// Log but continue if image insertion fails
			log.Printf("Failed to insert image %s: %v", imageURL, err)
		}
	}

	return tx.Commit()
}

// GetStatus returns the current status of the crawler
func (c *Crawler) GetStatus() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	return map[string]interface{}{
		"isRunning":   c.isRunning,
		"queueSize":   len(c.queue),
		"visitedSize": len(c.visited),
		"config":      c.config,
		"domains":     c.domains,
		"activityLog": c.activityLog,
	}
}

// UpdateConfig updates the crawler configuration
func (c *Crawler) UpdateConfig(config Config) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.config = config
}

// AddDomain adds a new domain to crawl
func (c *Crawler) AddDomain(domainURL string) (int, error) {
	// Validate URL
	_, err := url.Parse(domainURL)
	if err != nil {
		return 0, fmt.Errorf("invalid URL: %v", err)
	}

	// Check if domain already exists
	var domainID int
	err = c.db.QueryRow("SELECT id FROM domains WHERE url = ?", domainURL).Scan(&domainID)
	if err == nil {
		// Domain exists, update its status to pending
		_, err = c.db.Exec(`
			UPDATE domains 
			SET status = 'pending', pages_found = 0
			WHERE id = ?
		`, domainID)
		if err != nil {
			return 0, err
		}

		// Update local copy
		c.mu.Lock()
		for i := range c.domains {
			if c.domains[i].ID == domainID {
				c.domains[i].Status = "pending"
				c.domains[i].PagesFound = 0
				break
			}
		}
		c.mu.Unlock()

		return domainID, nil
	} else if err != sql.ErrNoRows {
		// Some other error
		return 0, err
	}

	// Domain doesn't exist, insert it
	result, err := c.db.Exec(`
		INSERT INTO domains (url, status, pages_found)
		VALUES (?, 'pending', 0)
	`, domainURL)
	if err != nil {
		return 0, err
	}

	// Get the new domain ID
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Add to local domains list
	c.mu.Lock()
	c.domains = append(c.domains, Domain{
		ID:         int(id),
		URL:        domainURL,
		Status:     "pending",
		PagesFound: 0,
	})
	c.mu.Unlock()

	return int(id), nil
}

// RemoveDomain removes a domain from the crawler
func (c *Crawler) RemoveDomain(id int) error {
	_, err := c.db.Exec(`DELETE FROM domains WHERE id = ?`, id)
	if err != nil {
		return err
	}

	// Remove from local list
	c.mu.Lock()
	for i := range c.domains {
		if c.domains[i].ID == id {
			c.domains = append(c.domains[:i], c.domains[i+1:]...)
			break
		}
	}
	c.mu.Unlock()

	return nil
}
