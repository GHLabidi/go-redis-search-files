package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// http reply struct
type Reply struct {
	Word          string
	Count         int
	Files         []string
	QueryDuration time.Duration
	SearchMode    string
}

// redis object struct
type RedisObject struct {
	Count int
	Files []string
}

// System specs struct
type SystemSpecs struct {
	CPUName  string
	CPUCores int
	RAMSize  string
	IsDocker bool
}

// all the files in the directory and subdirectories (to eliminate the need to traverse the directory again and again)
var (
	allFiles      []string
	allFilesMutex = &sync.Mutex{}
	// redis client
	rdb *redis.Client

	// context (for redis)
	ctx context.Context
)

func main() {

	// watch the data folder for changes
	go watchDataFolder("data")
	// get all file paths from a `rootDir`
	var rootDir string = "data"

	// get all the files at the start to avoid traversing the directory again and again
	getAllFilesPaths(rootDir)

	fmt.Println("Total files: ", len(allFiles))

	// start redis client
	startRedisClient()

	// start http server
	startHTTPServer()

}

// getAllFilesPaths: gets all the files paths from a directory and subdirectories
func getAllFilesPaths(rootDir string) {

	// temporary list of files
	var tmpFiles []string

	err := filepath.Walk(rootDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("error walking the path %q: %v\n", rootDir, err)
			return err
		}
		// if the path is not a directory, append it to the list of files
		if !info.IsDir() {
			tmpFiles = append(tmpFiles, path)
		}
		return nil
	})
	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", rootDir, err)
		return

	}
	// lock the list of files and set to the temporary list of files
	allFilesMutex.Lock()
	defer allFilesMutex.Unlock()
	allFiles = tmpFiles
}

// check error function to reduce code
func checkError(e error, w http.ResponseWriter) {
	if e != nil {
		http.Error(w, e.Error(), http.StatusInternalServerError)
		return

	}
}

func startRedisClient() {
	// load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")

	}
	// convert redis db to int
	db, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil {
		fmt.Println("Error converting REDIS_DB to integer")
	}
	// create redis client
	rdb = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	})

	ctx = context.Background()

	// ping redis to check if it is connected
	ping, err := rdb.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Error connecting to redis: ", err)
	} else {
		fmt.Println("Connected to redis: ", ping)
	}

}

func startHTTPServer() {

	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	port := os.Getenv("PORT")

	// api routes
	http.HandleFunc("/search", searchHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/help", helpHandler)
	http.HandleFunc("/system-specs", systemSpecsHandler)

	fmt.Println("Starting server on port: ", port)
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Error starting server: ", err)
	}
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	// get the word from the request
	word := r.URL.Query().Get("word")
	// forceSearch is used to force search the word in the files (skip redis)
	forceSearch := r.URL.Query().Get("forceSearch") == "true" // default is false
	// searchMode is used to select the search mode (simple or concurrent) (default is simple)
	searchMode := r.URL.Query().Get("searchMode") // default is simple
	if searchMode == "" {
		searchMode = "simple"
	}
	// cast concurrentThreadsParam to int
	concurrentThreadsParam := r.URL.Query().Get("concurrentThreads") // default is 100
	if concurrentThreadsParam == "" {
		concurrentThreadsParam = "100"
	}
	var concurrentThreads int
	_, err := fmt.Sscanf(concurrentThreadsParam, "%d", &concurrentThreads)
	if err != nil {
		http.Error(w, "Invalid concurrent threads", http.StatusBadRequest)
		return
	}

	if word == "" {
		http.Error(w, "Please provide a word to search", http.StatusBadRequest)
		return
	}

	var reply Reply

	var elapsedTime time.Duration
	startTime := time.Now()

	// if forceSearch is false, check if the word is in redis
	if !forceSearch {
		// check if the word is in redis and convert bytes to RedisObject
		val, err := rdb.Get(ctx, word).Bytes()
		if err == nil {
			// word found in redis

			elapsedTime = time.Since(startTime)

			// decode the bytes to RedisObject
			var redisObj RedisObject
			dec := gob.NewDecoder(bytes.NewReader(val))
			err = dec.Decode(&redisObj)
			checkError(err, w)

			// reply struct
			reply = Reply{
				Word:          word,
				Count:         redisObj.Count,
				Files:         redisObj.Files,
				QueryDuration: elapsedTime,
				SearchMode:    "redis",
			}
		}
	}

	if reply.Word == "" { // word not found in redis or forceSearch is true
		// word count
		var count int
		// list of files
		var list []string

		// make a copy of allFiles in case the list is updated by another thread
		allFilesMutex.Lock()
		filesToSearch := make([]string, len(allFiles))
		copy(filesToSearch, allFiles)
		allFilesMutex.Unlock()

		if searchMode == "concurrent" {
			count, list = concurrentSearchCount(word, concurrentThreads, filesToSearch)
		} else {
			count, list = simpleSearchCount(word, filesToSearch)
		}

		elapsedTime = time.Since(startTime)

		// reply struct
		reply = Reply{
			Word:          word,
			Count:         count,
			Files:         list,
			QueryDuration: elapsedTime,
			SearchMode:    searchMode,
		}

		// encode the reply struct to bytes
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		err := enc.Encode(RedisObject{Count: count, Files: list})
		checkError(err, w)
		err = rdb.Set(ctx, word, buf.Bytes(), 0).Err()
		checkError(err, w)
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// simpleSearchCount: loops through all the
func simpleSearchCount(word string, filesToSearch []string) (int, []string) {
	var count int = 0

	var files []string

	for _, file := range filesToSearch {
		// read file content
		content, err := os.ReadFile(file)
		if err != nil {
			// skip the file
			continue
		}
		// count the word in the file
		if c := strings.Count(string(content), word); c > 0 {
			count += c
			files = append(files, file)
		}

	}
	return count, files
}

// concurrentSearchCount: splits the files into chunks and searches concurrently in each chunk (using goroutines)
func concurrentSearchCount(word string, concurrentThreads int, filesToSearch []string) (int, []string) {
	var count int = 0
	var files []string

	// split the files into chunks
	chunks := splitFilesIntoChunks(filesToSearch, concurrentThreads)

	// create a channel to receive the results
	chCount := make(chan int, len(chunks))
	chFile := make(chan []string, len(chunks))

	// create a wait group to wait for all goroutines to complete
	var wg sync.WaitGroup

	// start a goroutine for each chunk
	for i, chunk := range chunks {
		wg.Add(1)

		go func(chunk []string, i int) {
			defer wg.Done()

			var c int = 0
			var foundFiles []string = []string{}
			for _, file := range chunk {
				// read file content
				content, err := os.ReadFile(file)
				if err != nil {
					// skip the file
					continue
				}
				// count the word in the file
				currentCount := strings.Count(string(content), word)
				c += currentCount
				if currentCount > 0 {
					foundFiles = append(foundFiles, file)
				}
			}
			// send the result to the channel
			chFile <- foundFiles
			chCount <- c

		}(chunk, i)
	}

	// wait for all goroutines to complete
	go func() {

		wg.Wait()
		// close the channels
		close(chCount)
		close(chFile)

	}()
	// receive the results from the channels
	for range chunks {
		count += <-chCount
	}
	for file := range chFile {
		for _, f := range file {
			files = append(files, f)
		}
	}

	return count, files
}

// splitFilesIntoChunks: splits the files into equal chunks. each concurrent thread will search in a chunk
func splitFilesIntoChunks(files []string, concurrentThreads int) [][]string {
	var chunks [][]string
	// get the chunk size
	chunkSize := len(files) / concurrentThreads

	// if the number of files is less than the number of concurrent threads, set the chunk size to 1
	if chunkSize == 0 {
		chunkSize = 1
	}
	// split the files into chunks
	for i := 0; i < len(files); i += chunkSize {
		end := i + chunkSize
		if end > len(files) {
			end = len(files)
		}
		chunks = append(chunks, files[i:end])
	}
	return chunks

}

func systemSpecsHandler(w http.ResponseWriter, r *http.Request) {
	// get the system specs
	specs := GetSystemInfo()

	// encode the specs to json
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(specs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

// watchDataFolder: watches the data folder for changes and updates the redis if a new file is created
func watchDataFolder(pathToWatch string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	defer watcher.Close()
	// Add the data folder to the watcher
	err = watcher.Add(pathToWatch)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// if a new file is created, handle it
			if event.Op&fsnotify.Create == fsnotify.Create {
				handleFileCreated(event.Name)
			}

			// TODO handle remove, modify, rename
			/*
				if event.Op&fsnotify.Write == fsnotify.Write {
					fmt.Println("modified file:", event.Name)
				}

				if event.Op&fsnotify.Remove == fsnotify.Remove {
					fmt.Println("removed file:", event.Name)
				}
				if event.Op&fsnotify.Rename == fsnotify.Rename {
					fmt.Println("renamed file:", event.Name)
				} */

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("error:", err)
		}
	}
}

// handleFileCreated: reads the file content and updates the redis
// TODO use redis pipelines
func handleFileCreated(path string) {

	// read file content from path
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Error reading file: ", err)
		return
	}

	// search all the words in the file and update redis
	keys, err := rdb.Keys(ctx, "*").Result()
	if err != nil {
		fmt.Println("Error getting redis keys: ", err)
		return
	}
	// check which keys are in the file and their count
	var keysCount map[string]int = make(map[string]int)
	for _, key := range keys {
		keysCount[key] = strings.Count(string(content), key)
	}
	fmt.Println("Keys count: ", keysCount)
	// update redis
	for key, count := range keysCount {
		if count > 0 {
			// decode the bytes to RedisObject
			var redisObj RedisObject
			val, err := rdb.Get(ctx, key).Bytes()
			if err != nil {
				fmt.Println("Error getting redis key: ", err)
				continue
			}
			dec := gob.NewDecoder(bytes.NewReader(val))
			err = dec.Decode(&redisObj)
			if err != nil {
				fmt.Println("Error decoding redis key: ", err)
				continue
			}
			// update the redis
			redisObj.Count += count
			redisObj.Files = append(redisObj.Files, path)
			// encode the reply struct to bytes
			var buf bytes.Buffer
			enc := gob.NewEncoder(&buf)
			err = enc.Encode(redisObj)
			if err != nil {
				fmt.Println("Error encoding redis object: ", err)
				continue
			}

			err = rdb.Set(ctx, key, buf.Bytes(), 0).Err()
			if err != nil {
				fmt.Println("Error setting redis key: ", err)
				continue
			}
		}
	}

	// lock the list of files and append the file path
	allFilesMutex.Lock()
	allFiles = append(allFiles, path)
	allFilesMutex.Unlock()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

func helpHandler(w http.ResponseWriter, r *http.Request) {
	// help message
	help := `Search API
		Usage: /search?word=<word>
		Parameters:
			word: the word to search
			forceSearch: force search the word in the files (skip redis)
			searchMode: select the search mode (simple or concurrent) (default is simple)
			concurrentThreads: number of concurrent threads to use (default is 100)
		Example: /search?word=hello&forceSearch=true&searchMode=concurrent&concurrentThreads=100
		`
	w.Write([]byte(help))
}

// GetSystemInfo returns the system specs and whether the code is running in Docker or natively
func GetSystemInfo() SystemSpecs {

	// Get the CPU name
	cpuName := "Unknown"
	cpuCores := -1
	if info, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(info), "\n") {
			if strings.HasPrefix(line, "model name") {
				cpuName = strings.TrimSpace(strings.Split(line, ":")[1])

			} else if strings.HasPrefix(line, "cpu cores") {
				cpuCores, err = strconv.Atoi(strings.TrimSpace(strings.Split(line, ":")[1]))
				if err != nil {
					cpuCores = 0
				}
			}

			if cpuName != "Unknown" && cpuCores != -1 {
				break
			}
		}
	}

	// Get the RAM size
	ramSize := "Unknown"
	if info, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(info), "\n") {
			if strings.HasPrefix(line, "MemTotal") {
				fields := strings.Fields(line)
				if len(fields) == 3 {
					size, err := strconv.Atoi(fields[1])
					if err == nil {
						ramSize = fmt.Sprintf("%.2f GB", float64(size)/1024/1024)
					}
				}
				break
			}
		}
	}

	// Check if running in Docker
	inDocker := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		inDocker = true
	}

	return SystemSpecs{
		CPUName:  cpuName,
		CPUCores: cpuCores,
		RAMSize:  ramSize,
		IsDocker: inDocker,
	}
}
