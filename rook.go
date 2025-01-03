package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/lmittmann/tint"
)

var (
	http_addr       string
	motion_map      map[string]int
	gmail_password  string
	gmail_username  string
	upgrader        = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	shutdownChannel = make(chan struct{})
)

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
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
}

type Context struct {
	MessageCount int
}

var CLI struct {
	MqttUsername      string `help:"MQTT username."`
	MqttPassword      string `help:"MQTT password."`
	MqttHostname      string `help:"MQTT hostname."`
	MqttPort          int    `help:"MQTT port." default:"1883"`
	GmailUsernameFile string `help:"Gmail username." default:"gmail_username.txt"`
	GmailPasswordFile string `help:"Gmail password." default:"gmail_password.txt"`
	HttpAddr          string `help:"HTTP address." default:":8080"`
}

var context *Context

func main() {
	// set global logger with custom options
	slog.SetDefault(slog.New(
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.Kitchen,
		}),
	))

	kong.Parse(&CLI)

	slog.Info("mqtt", "mqtt_server", CLI.MqttHostname)
	slog.Info("mqtt", "mqtt_usernane", CLI.MqttUsername)
	if CLI.MqttPassword == "" {
		slog.Error("mqtt", "mqtt_password", "Not set")
	}
	slog.Info("mqtt", "mqtt_port", CLI.MqttPort)
	slog.Info("gmail", "gmail_username_file", CLI.GmailUsernameFile)
	slog.Info("gmail", "gmail_password_file", CLI.GmailPasswordFile)
	slog.Info("http", "http_addr", CLI.HttpAddr)
	http_addr = CLI.HttpAddr

	if CLI.MqttHostname == "" || CLI.MqttUsername == "" || CLI.MqttPassword == "" {
		panic("Missing params")
	}
	context = &Context{MessageCount: 0}

	dat, err := os.ReadFile(CLI.GmailUsernameFile)
	if err != nil {
		panic(err)
	}
	gmail_username = strings.TrimSpace(string(dat))

	dat2, err2 := os.ReadFile(CLI.GmailPasswordFile)
	if err2 != nil {
		panic(err2)
	}
	gmail_password = strings.TrimSpace(string(dat2))

	motion_map = make(map[string]int)

	hostname, err := os.Hostname()
	if err != nil {
		slog.Error("Hostname", "error", err)
		os.Exit(1)
	}

	opts := MQTT.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:%d", CLI.MqttHostname, CLI.MqttPort))
	opts.SetClientID("rook_" + hostname)
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.SetUsername(CLI.MqttUsername)
	opts.SetPassword(CLI.MqttPassword)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		slog.Error("Mqtt", "connect", token.Error())
		os.Exit(1)
	}

	sub(client)

	go startHTTPServer()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// TODO Add a little REST API to get status
	slog.Info("Waiting for messages.")
	<-c
	slog.Info("Exiting gracefully.")
	// shutdownChannel <- struct{}{}

	client.Disconnect(250)
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	slog.Info("serveWs", "ws", "upgrade")
	ws, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		slog.Error("serveWs", "upgrade:", err)
		return
	}

	var status Status
	status.Status = "ok"

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer ws.Close()

	for {
		select {
		case <-ticker.C:
			slog.Info("serveWS", "ws", "write msg")
			status.MessageCount = context.MessageCount
			// context.MessageCount++

			if err := ws.WriteJSON(status); err != nil {
				slog.Error("serveWs", "WriteJSON:", err)
				// shutdownChannel <- struct{}{}
				return
			}
		case <-shutdownChannel:
			slog.Info("serveWS", "ws", "shutdown")
			return
		}
	}
}

func startHTTPServer() {
	r := mux.NewRouter()

	r.HandleFunc("/status", statusHandler).Methods("GET")
	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/ws", serveWs)
	http.Handle("/", r)
	http.ListenAndServe(http_addr, nil)
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
	slog.Debug("MQTT", "topic", msg.Topic())
	slog.Debug("MQTT", "payload", string(msg.Payload()))
	context.MessageCount++

	// Parse the JSON payload
	var payload Payload
	err := json.Unmarshal(msg.Payload(), &payload)
	if err != nil {
		fmt.Printf("Error parsing JSON: %s\n", err)
		return
	}

	// Use the parsed data
	slog.Debug("Parsed payload", "payload", payload)

	val, ok := motion_map[msg.Topic()]
	if (ok && val != payload.Motion) || !ok {
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
	slog.Info("Subscribed", "topic", topic)
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

	slog.Info("Email sent successfully")
	return nil
}
