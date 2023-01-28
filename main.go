package main

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"gopkg.in/irc.v3"
)

const (
	SITENAME = "p2irc"
	// HTML file to responde on get request
	INDEX_HTML = "index.html"
	// If this timeout is reached (seconds) while connecting to the irc, the program will exit
	TIMEOUT = 10
	// IP rate limiting. If set to 0 will be ignored
	MAX_PER_MINUTE = 2
	// You don't have to care about this if not using ip limiting
	REDIS_ADDR       = "localhost:6379"
	REDIS_KEY_PREFIX = "sendirc_"
)

var DEFAULT = []string{"irc.dot.org.es:6667", "romanian"}
var SHORTCUTS = map[string][]string{
	"linux": {"irc.libera.chat:6667", "linux"},
	"ro":    DEFAULT,
}

type RequestParam struct {
	method       string
	path         []string
	query        string
	remoteAddr   string
	nick         string
	content_type string
}

func getRequest() RequestParam {
	var path []string
	for _, p := range strings.Split(strings.TrimPrefix(os.Getenv("REQUEST_URI"), "/"), "/") {
		if len(p) > 0 {
			path = append(path, p)
		}
	}

	nick := strings.TrimSpace(os.Getenv("HTTP_NICK"))
	if len(nick) == 0 {
		nick = SITENAME
	}
	return RequestParam{
		method:       os.Getenv("REQUEST_METHOD"),
		path:         path,
		remoteAddr:   os.Getenv("REMOTE_ADDR"),
		nick:         nick,
		content_type: os.Getenv("HTTP_CONTENT_TYPE"),
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

func ircSend(conn net.Conn, server string, channel string, message string, nick string) {
	messages := strings.Split(message, "\n")

	config := irc.ClientConfig{
		Nick: nick,
		User: SITENAME,
		Name: SITENAME,
		Handler: irc.HandlerFunc(func(c *irc.Client, m *irc.Message) {
			fmt.Println(m)
			if m.Command == "001" {
				// 001 is a welcome event, so we join channels there
				if channel[0] == '-' {
					for _, msg := range messages {
						c.Write("PRIVMSG " + channel[1:] + " :" + msg)
					}
				}
				c.Write("JOIN #" + channel)
			} else if m.Command == "JOIN" && c.FromChannel(m) {
				for _, msg := range messages {
					c.WriteMessage(&irc.Message{
						Command: "PRIVMSG",
						Params: []string{
							m.Params[0],
							msg,
						},
					})
				}
				conn.Close()
				fmt.Printf("Sent successfully!\n")
				// exit program
				os.Exit(0)
			}
		})}

	// Create the client
	client := irc.NewClient(conn, config)
	err := client.Run()
	if err != nil {
		fmt.Printf("Failed to connect to that irc server!")
		return
	}
}

func errMessage() {
	fmt.Printf("Invalid request\nUsage example: cat /etc/pulse/default.pa | curl --data-binary @- " + SITENAME + "/irc.dot.org.es:6667/romanian\nAvailable shortcuts are: ")
	for k, v := range SHORTCUTS {
		fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
	}
}

func rate_limit_apply(request RequestParam) bool {
	if MAX_PER_MINUTE == 0 {
		return true
	}

	// Check if we have reached the limit per minute for the address
	var ctx = context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     REDIS_ADDR,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	key := REDIS_KEY_PREFIX + request.remoteAddr

	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		// Key does not exist
		rdb.Set(ctx, key, 1, time.Minute)

	} else if err != nil {
		fmt.Printf("Error acessing database")
		return false

	} else {
		// Key exists, check if we have reached the limit
		count, _ := strconv.Atoi(val)
		if count >= MAX_PER_MINUTE {
			fmt.Printf("You have reached the limit of messages per minute. Please try again later.")
			// renew it on redis
			rdb.Set(ctx, key, count, time.Minute)
			return false
		}
		// Increment counter
		count += 1
		rdb.Set(ctx, key, count, time.Minute)
	}

	return true

}

func main() {
	request := getRequest()
	fmt.Printf("Content-Type: text/html\n\n\n")

	if request.method == "GET" {
		sendHTMLFile(INDEX_HTML)
		return
	}

	if request.method != "POST" {
		errMessage()
		return
	}

	message := getBody()
	fmt.Println("message: " + message)

	server := ""
	channel := ""

	switch len(request.path) {
	case 0:
		server = DEFAULT[0]
		channel = DEFAULT[1]
	case 1:
		if _, ok := SHORTCUTS[request.path[0]]; ok {
			server = SHORTCUTS[request.path[0]][0]
			channel = SHORTCUTS[request.path[0]][1]
		} else {
			errMessage()
			return
		}
	case 2:
		server = request.path[0]
		channel = request.path[1]
	default:
		errMessage()
		return
	}

	// If server doesn't end with :port_number, default to :6667
	if !strings.Contains(server, ":") {
		server += ":6667"
	}

	fmt.Printf("Sending to %s at #%s\n", server, channel)

	conn, err := net.Dial("tcp", server)
	if err != nil {
		fmt.Printf("Failed to connect to that irc server!")
		return
	}

	if !rate_limit_apply(request) {
		return
	}

	_, ok := Timeout(TIMEOUT, func() bool {
		ircSend(conn, server, channel, message, request.nick)
		return true
	})
	if !ok {
		fmt.Printf("Timeout sending message. Please try again later.")
	}
}
