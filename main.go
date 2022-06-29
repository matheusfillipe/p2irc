package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/irc.v3"
)

const (
	MAX_LEN      = 400
	INDEX_HTML   = "index.html"
	PASTEBIN_URL = "http://ix.io"
  TIMEOUT = 10
  )
var SHORTCUTS = map[string][]string{
    "linux": {"irc.libera.chat:6667", "linux"},
  "ro": {"irc.dot.org.es:6667", "romanian"},
  }

type RequestParam struct {
	method string
	path   []string
	query  string
}

func getRequest() RequestParam {
	return RequestParam{
		method: os.Getenv("REQUEST_METHOD"),
		path:   strings.Split(strings.TrimPrefix(os.Getenv("REQUEST_URI"), "/"), "/"),
	}
}

func getBody() string {
	body, _ := ioutil.ReadAll(os.Stdin)
	return string(body)
}

func sendHTMLFile(file string) {
	f, _ := os.Open(file)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
}

// Paste to ix.io and get url
func paste(doc string) (string, bool) {
	// Create payload form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormField("f:1")
	io.Copy(part, strings.NewReader(doc))
	writer.Close()

	req, _ := http.NewRequest("POST", PASTEBIN_URL, body)
	req.Header.Add("Content-Type", writer.FormDataContentType())

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "Sorry but an error occured: " + err.Error(), false
	}
	defer resp.Body.Close()

	rbody, _ := ioutil.ReadAll(resp.Body)
	return string(rbody), true
}

// Run a function and return nil, false if it timeouts. otherwise returns f(), true
// timeout in seconds
func Timeout[R any](timeout int, f func() R) (R, bool) {
	c := make(chan int)
	resc := make(chan R)
	go func() {
		time.Sleep(time.Duration(timeout) * time.Second)
		c <- 1
	}()
	go func() {
		resc <- f()
	}()

	select {
	case res := <-resc:
		return res, true
	case <-c:
		var res R
		return res, false
	}
}

func ircSend(server string, channel string, message string) {
	conn, err := net.Dial("tcp", server)
	if err != nil {
		fmt.Printf("Failed to connect to that irc server!")
		return
	}

	config := irc.ClientConfig{
		Nick: "sendirc_bot",
		User: "sendirc.tk",
		Name: "sendirc.tk",
		Handler: irc.HandlerFunc(func(c *irc.Client, m *irc.Message) {
			if m.Command == "001" {
				// 001 is a welcome event, so we join channels there
				c.Write("JOIN #" + channel)
			} else if m.Command == "JOIN" && c.FromChannel(m) {
				c.WriteMessage(&irc.Message{
					Command: "PRIVMSG",
					Params: []string{
						m.Params[0],
						message,
					},
				})
				conn.Close()
				fmt.Printf("Sent successfully!\n")
				// exit program
				os.Exit(0)
			}
		})}

	// Create the client
	client := irc.NewClient(conn, config)
	err = client.Run()
	if err != nil {
		fmt.Printf("Failed to connect to that irc server!")
		return
	}
}

func main() {
	request := getRequest()
	fmt.Printf("Content-Type: text/html\n\n\n")

	if request.method == "GET" {
		sendHTMLFile(INDEX_HTML)
		return
	}

	// fmt.Printf("%+v\n", request)
	// fmt.Print("---\n", body)
	message := getBody()
	if len(message) > MAX_LEN {
		var ok = true
		message, ok = paste(message)
		if !ok {
			return
		}
	}

  server := ""
  channel := ""
  switch len(request.path) {
  case 1:
    if _, ok := SHORTCUTS[request.path[0]]; ok {
      server = SHORTCUTS[request.path[0]][0]
      channel = SHORTCUTS[request.path[0]][1]
    } else {
      fmt.Printf("Invalid request\nUsage example: cat /etc/pulse/default.pa | curl -d @- sendirc.tk/irc.dot.org.es:6667/romanian\nAvailable shortcuts are: ")
        return
    }
  case 2:
      server = request.path[0]
      channel = request.path[1]
  default:
      fmt.Printf("Invalid request\nUsage example: cat /etc/pulse/default.pa | curl -d @- sendirc.tk/irc.dot.org.es:6667/romanian\nAvailable shortcuts are: ")
      for k, v := range SHORTCUTS {
        fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
      }
        return
  }
	fmt.Printf("Sending to %s at #%s\n", server, channel)

	Timeout(TIMEOUT, func() bool {
		ircSend(server, channel, message)
		return true
	})
}
