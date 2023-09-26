package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// all the files in the directory and subdirectories (to eliminate the need to traverse the directory again and again)
var allFiles []string

// redis client
var rdb *redis.Client

// context (for redis)
var ctx context.Context

// constants
const (
	// redis
	redisHost = "localhost"
	redisPort = "6379"
	redisPass = ""
	redisDB   = 0
)

// http reply struct
type Reply struct {
	Word          string
	Count         int
	Files         []string
	QueryDuration time.Duration
}

// redis object struct
type RedisObject struct {
	Count int
	Files []string
}

func main() {
	// get all file paths from a `rootDir`
	var rootDir string = "data"
	// if you want to skip some files or directories
	var skipList []string = []string{}
	// get all the files at the start to avoid traversing the directory again and again
	allFiles = getAllFilesPaths(rootDir, skipList)

	// start redis client
	startRedisClient()

	// start http server
	startHTTPServer()
}

// check error function to reduce code
func checkError(e error, w http.ResponseWriter) {
	if e != nil {
		http.Error(w, e.Error(), http.StatusInternalServerError)
		return

	}
}

func startRedisClient() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     redisHost + ":" + redisPort,
		Password: redisPass,
		DB:       redisDB,
	})

	ctx = context.Background()
}

func startHTTPServer() {
	if len(os.Args) < 2 {
		fmt.Println("Please provide a port number")
		fmt.Println("Usage: go run main.go <port>")
		fmt.Println("Or for compiled binaries: ./binary-name <port>")
		return
	}

	port := os.Args[1]

	fmt.Println("Starting server on port: ", port)
	http.HandleFunc("/search", searchHandler)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Println("Error starting server: ", err)
	}
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	// get the word from the request
	word := r.URL.Query().Get("word")
	if word == "" {
		http.Error(w, "Please provide a word to search", http.StatusBadRequest)
		return
	}
	// forceSearch is used to force search the word in the files (skip redis)
	forceSearch := r.URL.Query().Get("forceSearch") == "true" // default is false

	startTime := time.Now()
	var elapsedTime time.Duration
	var reply Reply
	// check if the word is in redis and convert bytes to RedisObject
	if !forceSearch {
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
			}
		}
	}

	if reply.Word == "" { // word not found in redis or forceSearch is true
		// word count
		count, list := simpleSearchCount(word)
		elapsedTime = time.Since(startTime)

		// reply struct
		reply = Reply{
			Word:          word,
			Count:         count,
			Files:         list,
			QueryDuration: elapsedTime,
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
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// simpleSearchCount: loops through all the
func simpleSearchCount(word string) (int, []string) {

	var count int = 0
	// TODO return the list of files
	var files []string
	for _, file := range allFiles {
		// read file content
		content, err := os.ReadFile(file)
		if err != nil {
			// skip the file
			//fmt.Println("Error reading file: ", err)
			continue
		}
		if c := strings.Count(string(content), word); c > 0 {
			count += c
			files = append(files, file)
		}

	}
	return count, files
}

func getAllFilesPaths(rootDir string, skipList []string) []string {
	var tmpList []string
	err := filepath.Walk(rootDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		// check if the file is in the skip list
		for _, skip := range skipList {
			if info.Name() == skip {
				// skip the directory
				return filepath.SkipDir
			}
		}
		if !info.IsDir() {
			tmpList = append(tmpList, path)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", rootDir, err)
		return nil
	}
	return tmpList

}
