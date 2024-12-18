package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"os/signal"
	"strings"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
)

var motion_map map[string]int
var gmail_password string
var gmail_username string

// Define a struct that matches the JSON payload structure
// {"encryption":false,"BTHome_version":2,"pid":198,"Battery":100,"Illuminance":0,"Motion":1,"addr":"e8:e0:7e:a6:ac:db","rssi":-56}

type Payload struct {
	Encryption    bool   `json:"encryption"`
	BTHomeVersion int    `json:"BTHome_version"`
	Pid           int    `json:"pid"`
	Battery       int    `json:"Battery"`
	Illuminance   int    `json:"Illuminance"`
	Motion        int    `json:"Motion"`
	Addr          string `json:"addr"`
	Rssi          int    `json:"rssi"`
}

type Status struct {
	Status string `json:"status"`
}

type Context struct {
	MessageCount int
}

var context *Context

// slog
// https://github.com/avelino/awesome-go
// https://gobyexample.com/

func main() {
	context = &Context{MessageCount: 0}
	username := flag.String("username", "foo", "MQTT username")
	password := flag.String("password", "bar", "MQTT password")
	mqtt_hostname := flag.String("mqtt_server", "homeassistant.local", "MQTT hostname")
	mqtt_port := flag.Int("mqtt_port", 1883, "MQTT port")
	gmail_username_file := flag.String("gmail_username_file", "gmail_username.txt", "Gmail username file")
	gmail_password_file := flag.String("gmail_password_file", "gmail_password.txt", "Gmail password file")
	flag.Parse()

	dat, err := os.ReadFile(*gmail_username_file)
	if err != nil {
		panic(err)
	}
	gmail_username = strings.TrimSpace(string(dat))

	dat2, err2 := os.ReadFile(*gmail_password_file)
	if err != nil {
		panic(err2)
	}
	gmail_password = strings.TrimSpace(string(dat2))

	motion_map = make(map[string]int)

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	opts := MQTT.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:%d", *mqtt_hostname, *mqtt_port))
	opts.SetClientID("rook_" + hostname)
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.SetUsername(*username)
	opts.SetPassword(*password)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	sub(client)

	go startHTTPServer()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// TODO Add a little REST API to get status
	fmt.Println("Waiting for messages...")
	<-c
	fmt.Println("\nExiting gracefully...")

	client.Disconnect(250)
}

func startHTTPServer() {
	r := mux.NewRouter()

	r.HandleFunc("/status", statusHandler).Methods("GET")
	r.HandleFunc("/", indexHandler).Methods("GET")
	http.Handle("/", r)
	http.ListenAndServe(":8080", nil)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// w.Header().Set("Content-Type", "application/text")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Message Count %d", context.MessageCount)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var status Status
	status.Status = "ok"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	json.NewEncoder(w).Encode(status)
}

var messagePubHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	log.Printf("DEBUG: TOPIC: %s\n", msg.Topic())
	log.Printf("DEBUG: MSG: %s\n", msg.Payload())
	context.MessageCount++

	// Parse the JSON payload
	var payload Payload
	err := json.Unmarshal(msg.Payload(), &payload)
	if err != nil {
		fmt.Printf("Error parsing JSON: %s\n", err)
		return
	}

	// Use the parsed data
	fmt.Printf("Parsed payload: %+v\n", payload)
	fmt.Printf("\tMotion: %d\n", payload.Motion)

	val, ok := motion_map[msg.Topic()]
	if (ok && val != payload.Motion) || !ok {
		fmt.Print("Send email\n")
		body := "Motion detected"
		if payload.Motion == 0 {
			body = "Motion cleared"
		}
		err = sendEmail(gmail_username, msg.Topic(), body)
		if err != nil {
			panic(err)
		}
	}

	motion_map[msg.Topic()] = payload.Motion
}

// TODO Parse motion detection and send email
func sub(client MQTT.Client) {
	topic := "mostert/motion/#"
	token := client.Subscribe(topic, 1, nil)
	token.Wait()
	fmt.Printf("Subscribed to topic: %s\n", topic)
}

func sendEmail(to, subject, body string) error {
	from := gmail_username
	password := gmail_password

	// Gmail SMTP server address
	smtpHost := "smtp.gmail.com"
	smtpPort := "465"

	// Message
	message := []byte("Subject: " + subject + "\r\n" +
		"\r\n" + body + "\r\n")

	// Authentication
	auth := smtp.PlainAuth("", from, password, smtpHost)

	// TLS config
	tlsconfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         smtpHost,
	}

	// Connect to the SMTP server
	conn, err := tls.Dial("tcp", smtpHost+":"+smtpPort, tlsconfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	// Authenticate
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	// Set the sender and recipient
	if err = client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	// Send the email body
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to send email body: %w", err)
	}
	_, err = w.Write(message)
	if err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close email body: %w", err)
	}

	// Close the connection
	client.Quit()

	fmt.Println("Email sent successfully")
	return nil
}
