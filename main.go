package main

import (
	"bufio"
	"context"
	"encoding/json"
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

	// Defautl nick for github webhooks
	WEBHOOK_NICK = "github"
)

var (
	DEFAULT   = []string{"irc.dot.org.es:6667", "romanian"}
	SHORTCUTS = map[string][]string{
		"linux": {"irc.libera.chat:6667", "linux"},
		"ro":    DEFAULT,
	}
	WEBHOOK_TEMPLATES = [...]WebHookTemplate{
		{
			Ref:    "refs/heads/",
			Format: "----------------------------------------------\n--> \x02%s \x02received a commit from \x1d%s \x1d\"%s\"\n--> %s",
			Paths:  []string{"repository.name", "sender.login", "head_commit.message", "repository.url"},
		},
		{
			Ref:    "refs/tags/v",
			Format: "---------------------------------------------\n--> \x02%s\x02 version \x02%s\x02 was just released by \x1d%s\x1d\n--> %s",
			Paths:  []string{"repository.name", "self", "sender.login", "repository.url"},
		},
	}
)

type WebHookTemplate struct {
	// template ref name that matches the webhook refs (/refs/heads/ or /refs/tags/)
	Ref string
	// Template formate string
	Format string
	// Template paths like repository.url
	Paths []string
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
	return RequestParam{
		method:       os.Getenv("REQUEST_METHOD"),
		path:         path,
		remoteAddr:   os.Getenv("REMOTE_ADDR"),
		nick:         nick,
		content_type: strings.ToLower(os.Getenv("HTTP_CONTENT_TYPE")),
	}
}

func getBody() string {
	body, _ := ioutil.ReadAll(os.Stdin)
	return string(body)
}

// / Parse the request as a json from github webhooks and returns formated message
func parseWebhook() string {
	decoder := json.NewDecoder(os.Stdin)
	var t map[string]interface{}
	err := decoder.Decode(&t)
	if err != nil {
		fmt.Printf("Failed to parse json: %s", err)
		// exit program
		os.Exit(0)
	}
	fmt.Println("Webhook detected")
	// template matching
	for _, template := range WEBHOOK_TEMPLATES {
		// If webhook ref starts with template ref
		if strings.Index(t["ref"].(string), template.Ref) == 0 {
			fmt.Printf("Template matched to %s\n", template.Ref)
			var elements []any
			for _, path := range template.Paths {
				// Loop over path split by . and get the value, exclude the last one
				keys := strings.Split(path, ".")
				prop := keys[len(keys)-1]
				value := ""
				if prop == "self" {
					fmt.Printf("Self\n")
					ref := strings.Split(t["ref"].(string), "/")
					fmt.Printf("ref: %s\n", ref)
					value = ref[len(ref)-1]
				} else {
					// Clone the map
					dict := make(map[string]interface{})
					for k, v := range t {
						dict[k] = v
					}
					for _, p := range keys[:len(keys)-1] {
						fmt.Printf("Path: %s\n", p)
						dict = dict[p].(map[string]interface{})
					}
					value = dict[prop].(string)
				}
				fmt.Printf("Value: %s\n", value)
				elements = append(elements, value)
			}
			return fmt.Sprintf(template.Format, elements...)
		}
	}
	fmt.Printf("No template matched for webhook")
	os.Exit(0)
	return ""
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

	message := ""
	if request.content_type == "application/json" {
		message = parseWebhook()
		if request.nick == "" {
			request.nick = WEBHOOK_NICK
		}
	} else {
		message = getBody()
		if request.nick == "" {
			request.nick = SITENAME
		}
	}

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

func getWebhookMessage() {
	panic("unimplemented")
}
