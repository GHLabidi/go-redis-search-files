# Go Redis Search Files
This is a work in progress
## Description
A tool to search for a word in a large number of files. It is written in Go and uses Redis to cash the search results.
## Usage
The tool has a /search endpoint that has two query parameters:
- word: the word to search for
- forceSearch (optional / default: false): if true, the tool will search for the word in all files and will not use the cached results
## How to run
### Prerequisites
- Go 1.14 or higher (needed for the go modules)
- Redis Server
### Steps
1. Clone the repository
2. Place all your files in the data directory
    - You can use this dataset: TODO Add link
3. Add redis server credentials to main.go
    - TODO move to .env file

4. Two options to run the go server
   - Option 1:
        - Run `go run main.go <port>` in the root directory
        - Example: `go run main.go 8080`
    - Option 2:
        - Run `go build` in the root directory
        - Run `./go-redis-search-files <port>`
        - Example: `./go-redis-search-files 8080`
5. Send a request to the server
    - Example: `curl -X GET "http://localhost:8080/search?word=The"`
        - Will search for the word "The" in all files if it is not cached
        - If it is cached, it will return the cached results with a much faster response time
    - Example: `curl -X GET "http://localhost:8080/search?word=The&forceSearch=true"`
        - Will search for the word "The" in all files and will not use the cached results

## Aim
The aim is to turn it into a file search engine that has multiple search options and can be used to search for a word in a large number of files and also able to serve files to the user.
## TODO
- [ ] Add more search options
- [ ] Add file serving
- [ ] Add performance tests
