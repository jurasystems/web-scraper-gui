package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var crawler *Crawler

func main() {
	// Initialize database
	db, err := initDB("search.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize crawler
	crawler = NewCrawler(db)

	// Set up Gin router
	router := gin.Default()

	// Serve static files
	router.Static("/static", "./static")

	// Load HTML templates
	router.LoadHTMLGlob("templates/*")

	// Dashboard route
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"title": "Crawler Dashboard",
		})
	})

	// API routes
	api := router.Group("/api")
	{
		// Get crawler status
		api.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, crawler.GetStatus())
		})

		// Start crawler
		api.POST("/start", func(c *gin.Context) {
			crawler.Start()
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Crawler started"})
		})

		// Stop crawler
		api.POST("/stop", func(c *gin.Context) {
			crawler.Stop()
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Crawler stopped"})
		})

		// Update crawler config
		// Update crawler config
		api.POST("/config", func(c *gin.Context) {
			var config Config

			// Debug log
			body, _ := io.ReadAll(c.Request.Body)
			log.Printf("Received config update: %s", string(body))
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

			if err := c.ShouldBindJSON(&config); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Debug log
			log.Printf("Parsed config: %+v", config)

			crawler.UpdateConfig(config)
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Configuration updated"})
		})
		// Domain management
		domains := api.Group("/domains")
		{
			// List all domains
			domains.GET("", func(c *gin.Context) {
				status := crawler.GetStatus()
				c.JSON(http.StatusOK, status["domains"])
			})

			// Add a new domain
			domains.POST("", func(c *gin.Context) {
				var input struct {
					URL string `json:"url" binding:"required"`
				}
				if err := c.ShouldBindJSON(&input); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}

				id, err := crawler.AddDomain(input.URL)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": "Domain added",
					"id":      id,
				})
			})

			// Import domains from CSV
			domains.POST("/import", func(c *gin.Context) {
				// Get file from form data
				file, _, err := c.Request.FormFile("csv_file")
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
					return
				}
				defer file.Close()

				// Parse CSV
				reader := csv.NewReader(file)

				// Track results
				results := struct {
					Total      int      `json:"total"`
					Added      int      `json:"added"`
					Failed     int      `json:"failed"`
					FailedURLs []string `json:"failedUrls"`
				}{0, 0, 0, []string{}}

				// Process CSV row by row
				for {
					record, err := reader.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						continue
					}

					// Skip empty rows
					if len(record) == 0 || record[0] == "" {
						continue
					}

					// Process URL
					url := strings.TrimSpace(record[0])
					results.Total++

					_, err = crawler.AddDomain(url)
					if err != nil {
						results.Failed++
						results.FailedURLs = append(results.FailedURLs, url+" ("+err.Error()+")")
					} else {
						results.Added++
					}
				}

				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": fmt.Sprintf("Imported %d of %d domains", results.Added, results.Total),
					"results": results,
				})
			})

			// Remove a domain
			domains.DELETE("/:id", func(c *gin.Context) {
				id := c.Param("id")
				var domainID int
				if _, err := fmt.Sscanf(id, "%d", &domainID); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain ID"})
					return
				}

				if err := crawler.RemoveDomain(domainID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": "Domain removed",
				})
			})
		}
	}

	// Run the server
	log.Println("Starting server on :8080...")
	router.Run(":8080")
}

// initDB initializes the database with required tables
func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create domains table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			last_crawl TIMESTAMP,
			status TEXT NOT NULL,
			pages_found INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		return nil, err
	}

	// Create pages table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			url TEXT NOT NULL UNIQUE,
			description TEXT,
			content TEXT,
			vote_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, err
	}

	// Create full_pages table for storing complete HTML
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS full_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			page_id INTEGER NOT NULL UNIQUE,
			html_content TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (page_id) REFERENCES pages (id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return nil, err
	}

	// Create images table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS images (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			url TEXT NOT NULL,
			image_url TEXT NOT NULL UNIQUE,
			description TEXT,
			alt_text TEXT,
			width INTEGER DEFAULT 0,
			height INTEGER DEFAULT 0,
			vote_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, err
	}

	// Create votes table if it doesn't exist
	// Create votes table if it doesn't exist
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS votes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	item_id INTEGER NOT NULL,
	item_type TEXT NOT NULL,
	vote_type INTEGER NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(item_id, item_type)
)
`)
	if err != nil {
		return nil, err
	}

	// Create indexes for better performance
	_, err = db.Exec(`
CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url);
CREATE INDEX IF NOT EXISTS idx_images_url ON images(url);
CREATE INDEX IF NOT EXISTS idx_images_image_url ON images(image_url);
CREATE INDEX IF NOT EXISTS idx_full_pages_page_id ON full_pages(page_id);
`)
	if err != nil {
		return nil, err
	}

	return db, nil
}
