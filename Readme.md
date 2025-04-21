# Web Crawler GUI

A simple web-based GUI interface for managing and controlling a web crawler built in Go.

![Web Crawler Dashboard](images/dashboard.png)

## ⚠️ IMPORTANT LEGAL NOTICE

**This software is provided for EDUCATIONAL PURPOSES ONLY**

By using this repository, code, or any related materials, you accept full responsibility for your actions. The authors and contributors assume ZERO LIABILITY and provide NO WARRANTY whatsoever. You are solely responsible for how you use this tool and for any consequences that may result from your use.

**All users must:**
- Comply with all applicable laws and regulations
- Respect website terms of service and robots.txt directives
- Obtain proper permission before crawling any website
- Use this tool responsibly and ethically

## Overview

This application provides a user-friendly dashboard to control a web crawler that can systematically browse websites, collect data, and store it in a SQLite database. The crawler is built with Go and uses a web-based interface powered by Gin, Bootstrap, and JavaScript.

## Features

- **Web-based dashboard** for controlling the crawler
- **Domain management** - add, remove, and import domains to crawl
- **Configuration options** - customize concurrency, delay, crawl depth
- **Real-time status updates** - view crawler activity and logs
- **Full-page download option** - store complete HTML content
- **SQLite database** for storing crawled data

## ⚠️ DEVELOPMENT STATUS

**This software is in EARLY DEVELOPMENT stage**

- Some features may be unstable or break unexpectedly
- The codebase needs significant cleanup
- Not recommended for production use
- Will soon transition to a stable version

## Installation

### Prerequisites

- Go 1.16+
- SQLite3

### Setup

1. Clone the repository:
git clone https://github.com/yourusername/web-crawler-gui.git
cd web-crawler-gui

2. Install dependencies:
go mod download

3. Run the application:
make run

4. Open your browser and navigate to:
http://localhost:8080

## Makefile Commands

run:
	go run .

build:
	go build -o dist/crawler

clean:
	rm -rf dist/

## Usage

1. **Start the application** using `make run`
2. **Add domains** to crawl through the dashboard
3. **Configure crawler settings** (concurrency, delay, depth)
4. **Start the crawler** using the "Start Crawler" button
5. **Monitor progress** in the activity log

## Configuration Options

- **Concurrency**: Number of simultaneous crawl operations (1-20)
- **Delay**: Wait time between requests in milliseconds (0-10000)
- **Max Depth**: How deep the crawler will follow links (1-10)
- **Stay in Domain**: Restrict crawling to the initial domain
- **Download Full Pages**: Store complete HTML content in database

## Database Schema

The application uses SQLite with the following tables:
- `domains`: Stores domains to be crawled
- `pages`: Stores information about crawled pages
- `full_pages`: Stores complete HTML content when enabled
- `images`: Stores information about images found during crawling
- `votes`: Stores user votes/ratings for pages and images

## Known Issues

- Configuration may not always persist between server restarts
- CSV import may fail with malformed URLs
- Activity log doesn't auto-scroll to the bottom
- Full page download may cause memory issues with large sites

## Future Improvements

- Add user authentication
- Implement scheduling for recurring crawls
- Add export functionality for crawled data
- Improve error handling and recovery
- Add proxies and user agent rotation

## License

MIT License - See LICENSE file for details

## Documentation

For more detailed documentation, please refer to:
https://github.com/jurasystems/web-scraper-gui