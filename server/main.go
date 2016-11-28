package main

import (
	"encoding/json"
	"fmt"
	"github.com/schollz/bol/ssed"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/schollz/bol/utils"
	"github.com/schollz/cryptopasta"
)

// https://gist.github.com/tristanwietsma/8444cf3cb5a1ac496203
type handler func(w http.ResponseWriter, r *http.Request)

var wd string
var apikeys = struct {
	sync.RWMutex
	m map[string]string
}{m: make(map[string]string)}

func main() {
	wd, _ = os.Getwd()
	http.HandleFunc("/", HandleLogin)
	http.HandleFunc("/login", HandleLoginAttempt)
	http.HandleFunc("/post", HandlePostAttempt)
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})
	http.HandleFunc("/repo", HandleRepo) // POST latest repo
	fmt.Println("Running on 0.0.0.0:9095")
	log.Fatal(http.ListenAndServe(":9095", nil))
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "login.html")
}

type loginInfo struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func HandlePostAttempt(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(w, "Error")
		return
	}
	if !strings.Contains(string(body), "=") {
		fmt.Fprintf(w, "Did you post?")
		return
	}
	lines := strings.Split(string(body), "=")
	apikey := strings.TrimSpace(strings.Split(lines[1], "document")[0])
	document := strings.TrimSpace(strings.Split(lines[2], "entry")[0])
	entry := strings.TrimSpace(strings.Split(lines[3], "data")[0])
	text := strings.Join(strings.Split(strings.TrimSpace(lines[4]), "\r"), "\n")
	var username, password string
	apikeys.Lock()
	if val, ok := apikeys.m[apikey]; ok {
		username = strings.Split(val, "=")[0]
		password = strings.Split(val, "=")[1]
	} else {
		apikeys.Unlock()
		http.ServeFile(w, r, "login.html")
		return
	}
	delete(apikeys.m, apikey)
	apikeys.Unlock()

	fmt.Println(username, password)
	go updateRepo(username, password, text, document, entry, "")
	fmt.Fprintf(w, "thanks")
}

func updateRepo(username, password, text, document, entry, date string) {
	var fs ssed.Fs
	fs.Init(username, "http://127.0.0.1:9095")
	fs.Open(password)
	fs.Update(text, document, entry, date)
	fs.Close()
}

func HandleLoginAttempt(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(w, "Error")
		return
	}
	if !strings.Contains(string(body), "&") {
		fmt.Fprintf(w, "Bad login attempt")
		return
	}
	data := strings.Split(string(body), "&")
	username := strings.TrimSpace(strings.Split(data[0], "=")[1])
	password := strings.TrimSpace(strings.Split(data[1], "=")[1])
	creds := make(map[string]string)
	loginData, _ := ioutil.ReadFile(path.Join(wd, "logins.json"))
	json.Unmarshal(loginData, &creds)
	authenticated := false

	if passwordHash, ok := creds[username]; ok {
		if cryptopasta.CheckPasswordHash([]byte(passwordHash), []byte(password)) == nil {
			authenticated = true
		}
	} else {
		log.Printf("LOGIN: User '%s' does not exist", username)
		w.WriteHeader(http.StatusNetworkAuthenticationRequired)
		io.WriteString(w, username+" does not exist")
		return
	}

	if authenticated {
		page, _ := ioutil.ReadFile("post.html")
		pageS := string(page)
		apikey := utils.RandStringBytesMaskImprSrc(30)
		apikeys.Lock()
		apikeys.m[apikey] = username + "=" + password
		apikeys.Unlock()
		pageS = strings.Replace(pageS, "keyXX", apikey, -1)
		fmt.Fprintf(w, "%s", pageS)
	} else {
		fmt.Fprintf(w, "Denied")
	}
}

func HandlePush(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	log.Println("Pushed new repo")
	username, password, _ := r.BasicAuth()
	log.Println(r.BasicAuth())
	creds := make(map[string]string)
	data, _ := ioutil.ReadFile(path.Join(wd, "logins.json"))
	json.Unmarshal(data, &creds)

	authenticated := false

	if passwordHash, ok := creds[username]; ok {
		if cryptopasta.CheckPasswordHash([]byte(passwordHash), []byte(password)) == nil {
			authenticated = true
		}
	} else {
		log.Printf("PUSH: User '%s' does not exist", username)
		w.WriteHeader(http.StatusNetworkAuthenticationRequired)
		io.WriteString(w, username+" does not exist")
		return
	}

	if authenticated {
		fileName := username + ".tar.bz2"

		// backup the previous
		if utils.Exists(fileName) {
			for i := 1; i < 1000000; i++ {
				newFileName := fileName + "." + strconv.Itoa(i)
				if utils.Exists(newFileName) {
					continue
				}
				utils.CopyFile(fileName, newFileName)
				break
			}
		}

		os.Remove(fileName)
		outFile, err := os.Create(fileName)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// handle err
		defer outFile.Close()
		_, err = io.Copy(outFile, r.Body)
		log.Printf("PUSH: Wrote file for '%s'\n", username)
		io.WriteString(w, "thanks\n")
	} else {
		log.Println("Incorect password, " + password)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "incorrect password")
	}

}

func HandleDelete(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	log.Println("Erasing repo")
	username, password, _ := r.BasicAuth()
	log.Println(r.BasicAuth())
	creds := make(map[string]string)
	data, _ := ioutil.ReadFile(path.Join(wd, "logins.json"))
	json.Unmarshal(data, &creds)
	authenticated := false

	if passwordHash, ok := creds[username]; ok {
		if cryptopasta.CheckPasswordHash([]byte(passwordHash), []byte(password)) == nil {
			authenticated = true
		}
	} else {
		log.Printf("DELETE: User '%s' does not exist", username)
		w.WriteHeader(http.StatusNetworkAuthenticationRequired)
		io.WriteString(w, username+" does not exist")
		return
	}

	if authenticated {
		fileName := username + ".tar.bz2"
		os.Remove(fileName)
	}

}

func HandlePull(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	username, _, _ := r.BasicAuth()
	log.Println("Got repo request from " + username)
	fileName := username + ".tar.bz2"
	if utils.Exists(fileName) {
		w.Header().Set("Content-Type", "octet-stream")
		file, _ := os.Open(fileName)
		io.Copy(w, file)
	} else {
		w.WriteHeader(http.StatusNoContent)
		io.WriteString(w, "repo does not exist")
	}

}

func HandleNew(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	username, password, _ := r.BasicAuth()
	hashedPassword, _ := cryptopasta.HashPassword([]byte(password))
	creds := make(map[string]string)

	if utils.Exists(path.Join(wd, "logins.json")) {
		data, _ := ioutil.ReadFile(path.Join(wd, "logins.json"))
		json.Unmarshal(data, &creds)
		if _, ok := creds[username]; ok {
			io.WriteString(w, username+" already exists")
			return
		}
	}
	creds[username] = string(hashedPassword)
	b, _ := json.MarshalIndent(creds, "", "  ")
	ioutil.WriteFile(path.Join(wd, "logins.json"), b, 0644)
	io.WriteString(w, "inserted new user, "+username)

}

func HandleRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		HandlePull(w, r)
	} else if r.Method == "POST" {
		HandlePush(w, r)
	} else if r.Method == "DELETE" {
		HandleDelete(w, r)
	} else if r.Method == "PUT" {
		HandleNew(w, r)
	}
}

func GetOnly(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			h(w, r)
			return
		}
		http.Error(w, "get only", http.StatusMethodNotAllowed)
	}
}

func PostOnly(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			h(w, r)
			return
		}
		http.Error(w, "post only", http.StatusMethodNotAllowed)
	}
}

func PutOnly(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			h(w, r)
			return
		}
		http.Error(w, "put only", http.StatusMethodNotAllowed)
	}
}

func DeleteOnly(h handler) handler {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			h(w, r)
			return
		}
		http.Error(w, "delete only", http.StatusMethodNotAllowed)
	}
}
