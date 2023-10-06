# Go Redis Search Files
This is a work in progress
## Description
A tool to search for a word in a large number of files either sequentially or by using goroutines. 
It is written in Go and uses Redis to cache the search results. 
## Aim
The goal is to eventually turn this into a distributed system that can be used to search for a word in a large number of files across multiple servers. As of now, it is a single server that uses Redis to cache the search results and can search for a word in a large number of files either sequentially or by using goroutines.
## API
### Search
- **URL**
    - /search
- **Method**
    - GET
- **URL Params**
    - **Required**
        - word=[string]
            - The word to search for
    - **Optional**
        - forceSearch=[bool] (default: false)
            - If true, will not use the cached results
        - searchMode=[string] (default: simple)
            - The search mode to use (simple, concurrent). Can be extended to add more search modes
            - Options:
                - simple
                    - Goes through each file and searches for the word
                - concurrent
                    - Uses goroutines to search for the word in each file.
        - concurrentThreads=[int] (default: 100)
            - The number of threads to use when searching in concurrent mode.
             
- **Success Response**
    - **Code:** 200
    - **Content:** 
        - ```json
               {
                    "Word": "thee",
                    "Count": 3,
                    "Files": [
                        "data/subfolder1/sample_text1.txt",
                        "data/subfolder2/sample_xml.xml",
                        "data/subfolder2/subsubfolder/sample_python.py",
                    ],
                    "QueryDuration": 283322650,
                    "SearchMode": "concurrent"
                }
            ```
    - **Response Description**
        - Word: The word that was searched for
        - Count: The number of times the word was found
        - Files: The files that the word was found in
        - QueryDuration: The time it took to search for the word (in nanoseconds)
        - SearchMode: The search mode that was used. Can be simple, concurrent, redis. **Will be renamed to ResultsSource**.
- **Sample Calls**
    - ```curl -X GET "http://localhost:8080/search?word=The"```
    - ```curl -X GET "http://localhost:8080/search?word=The&forceSearch=true"```
    - ```curl -X GET "http://localhost:8080/search?word=The&searchMode=concurrent"```
    - ```curl -X GET "http://localhost:8080/search?word=The&searchMode=concurrent&concurrentThreads=100"```
    - ```curl -X GET "http://localhost:8080/search?word=The&searchMode=concurrent&concurrentThreads=100&forceSearch=true"```
### Health
- **URL**
    - /health
- **Method**
    - GET
- **Success Response**

    - **Code:** 200
    - **Content:** 
        - ```text
            OK
          ```
    - **Response Description**
        - Returns OK if the server is running. Will be extended to return more information about the server in json format.

- **Sample Call**
    - ```curl -X GET "http://localhost:8080/health"```
### System Specs
- **URL**
    - /system-specs
- **Method**
    - GET
- **Success Response**
    - **Code:** 200
    - **Content:** 
        - ```json
            {
            "CPUName": "AMD Ryzen 7 3700X 8-Core Processor",
            "CPUCores": 8,
            "RAMSize": "7.13 GB",
            "IsDocker": false
            }
            ```
### Help
- **URL**
- **Method**
    - GET
- **Success Response**
    - **Code:** 200
    - **Content:**
        - ```text
            Text response
          ```
## How to run
### Prerequisites
- Go 1.14 or higher (needed for the go modules)
- Redis Server
### Steps
1. Clone the repository
2. Place all your files in the data directory
3. Copy and edit the .env.example file to .env
    - Set the REDIS_HOST to the host of your redis server
    - Set the REDIS_PORT to the port of your redis server
    - Set the REDIS_DB to the db number of your redis server
    - Set the REDIS_PASSWORD to the password of your redis server
    - Set the PORT on which the server will run.

4. Two options to run the go server
   - Option 1:
        - Run `go run main.go` in the root directory
    - Option 2:
        1. Run `go build` in the root directory
        2. Run `./go-redis-search-files`
        3. Example: `./go-redis-search-files`

## Known Issues
### Handling multiple concurrent requests
- **Issue:** If **n** clients lookup a word that is not cached at the same time, the program will launch **n** search operations. This can significantly slow down the system.
- **Potential Solutions:**
    - **Keeping track of the words being searched:** We can use a list to keep track of the words currently being searched and use a mutex to make the list thread safe. If the word is in the list (meaning it is being searched for) then wait for the result to be passed through a channel instead of searching for the word again.
    - **Limit the total number of possible concurrent requests:** Use some mechanism to limit the maximum concurrent requests that can be served at once, if the maximum is reached/ exceeded, set a timeout for the request
