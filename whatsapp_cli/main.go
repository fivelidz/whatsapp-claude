// whatsapp-claude - Connect WhatsApp to Claude AI
// Built by Qalarc - https://qalarc.com
//
// Uses the whatsmeow library to interface with WhatsApp Web protocol.
// This binary is the transport layer - it handles login, sending, and receiving.
//
// Commands:
//   whatsapp-claude login              - Login via QR code or phone pairing
//   whatsapp-claude receive            - Listen for incoming messages (JSON to stdout)
//   whatsapp-claude send <jid> <msg>   - Send a text message
//   whatsapp-claude send-file <jid> <path> [caption] - Send a file
//   whatsapp-claude logout             - Log out and clear session
//   whatsapp-claude status             - Check connection status
//
// Built with: https://github.com/tulir/whatsmeow
// License: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Message represents an incoming or outgoing message in JSON format
type Message struct {
	Type        string           `json:"type"`
	Sender      string           `json:"sender"`
	SenderJID   string           `json:"sender_jid"`
	SenderName  string           `json:"sender_name,omitempty"`
	Chat        string           `json:"chat"`
	ChatJID     string           `json:"chat_jid"`
	Message     string           `json:"message,omitempty"`
	Timestamp   int64            `json:"timestamp"`
	MessageID   string           `json:"message_id"`
	IsGroup     bool             `json:"is_group"`
	Attachments []AttachmentInfo `json:"attachments,omitempty"`
	Quote       *QuoteInfo       `json:"quote,omitempty"`
}

type AttachmentInfo struct {
	Type      string `json:"type"`
	MimeType  string `json:"mime_type"`
	Filename  string `json:"filename,omitempty"`
	Size      uint64 `json:"size,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
}

type QuoteInfo struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`
	Text      string `json:"text,omitempty"`
}

var client *whatsmeow.Client
var dataDir string

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Data directory: set via env var or defaults to ./auth next to binary
	dataDir = os.Getenv("WHATSAPP_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(filepath.Dir(os.Args[0]), "..", "auth")
	}
	os.MkdirAll(dataDir, 0700)

	command := os.Args[1]

	switch command {
	case "login":
		doLogin()
	case "receive":
		doReceive()
	case "send":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: whatsapp-claude send <phone/jid> <message>")
			os.Exit(1)
		}
		doSend(os.Args[2], strings.Join(os.Args[3:], " "))
	case "send-file":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: whatsapp-claude send-file <phone/jid> <filepath> [caption]")
			os.Exit(1)
		}
		caption := ""
		if len(os.Args) > 4 {
			caption = strings.Join(os.Args[4:], " ")
		}
		doSendFile(os.Args[2], os.Args[3], caption)
	case "logout":
		doLogout()
	case "status":
		doStatus()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`whatsapp-claude - Connect WhatsApp to Claude AI
Built by Qalarc - https://qalarc.com

Usage:
  whatsapp-claude login                          Login via QR code
  whatsapp-claude receive                        Listen for messages (JSON to stdout)
  whatsapp-claude send <phone/jid> <message>     Send a text message
  whatsapp-claude send-file <phone/jid> <path> [caption]  Send a file
  whatsapp-claude logout                         Log out and clear session
  whatsapp-claude status                         Check connection status

Environment:
  WHATSAPP_DATA_DIR    Directory for session/auth data (default: ./auth)
  WHATSAPP_PHONE       Phone number for pairing code login (alternative to QR)
  WHATSAPP_LOG_LEVEL   Log level: DEBUG, INFO, WARN (default: WARN)

Phone number format: Include country code without + (e.g., 14155551234)

Quick Start:
  1. Run 'whatsapp-claude login' and scan the QR code
  2. Run the bridge: python3 bridge.py
`)
}

func getClient() *whatsmeow.Client {
	ctx := context.Background()

	logLevel := os.Getenv("WHATSAPP_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "WARN"
	}
	dbLog := waLog.Stdout("Database", logLevel, true)

	dbPath := filepath.Join(dataDir, "whatsapp.db")
	container, err := sqlstore.New(ctx, "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get device: %v\n", err)
		os.Exit(1)
	}

	clientLog := waLog.Stdout("Client", logLevel, true)
	client = whatsmeow.NewClient(deviceStore, clientLog)

	return client
}

func normalizeJID(input string) types.JID {
	input = strings.TrimSpace(input)
	input = strings.ReplaceAll(input, " ", "")
	input = strings.ReplaceAll(input, "-", "")
	input = strings.ReplaceAll(input, "+", "")

	if strings.Contains(input, "@") {
		jid, _ := types.ParseJID(input)
		return jid
	}

	return types.NewJID(input, types.DefaultUserServer)
}

func doLogin() {
	client := getClient()

	if client.Store.ID != nil {
		err := client.Connect()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(`{"status": "already_logged_in", "phone": "` + client.Store.ID.User + `"}`)
		client.Disconnect()
		return
	}

	// Pairing code login (phone number provided via env)
	phoneNumber := os.Getenv("WHATSAPP_PHONE")
	if phoneNumber != "" {
		doPairingCodeLogin(client, phoneNumber)
		return
	}

	// QR code login
	qrChan, _ := client.GetQRChannel(context.Background())
	err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			fmt.Fprintln(os.Stderr, "\nScan this QR code with WhatsApp on your phone:")
			fmt.Fprintln(os.Stderr, "Settings -> Linked Devices -> Link a Device")
			fmt.Fprintln(os.Stderr, "")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stderr)
			fmt.Fprintln(os.Stderr, "")
		case "success":
			fmt.Println(`{"status": "login_success", "phone": "` + client.Store.ID.User + `"}`)
			fmt.Fprintln(os.Stderr, "Login successful! Waiting for connection to stabilize...")
			time.Sleep(5 * time.Second)
			client.Disconnect()
			return
		case "timeout":
			fmt.Println(`{"status": "timeout", "error": "QR code expired - run login again"}`)
			client.Disconnect()
			os.Exit(1)
		}
	}
}

func doPairingCodeLogin(client *whatsmeow.Client, phoneNumber string) {
	phoneNumber = strings.ReplaceAll(phoneNumber, "+", "")
	phoneNumber = strings.ReplaceAll(phoneNumber, " ", "")
	phoneNumber = strings.ReplaceAll(phoneNumber, "-", "")

	err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}

	code, err := client.PairPhone(context.Background(), phoneNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to request pairing code: %v\n", err)
		client.Disconnect()
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n========================================\n")
	fmt.Fprintf(os.Stderr, "PAIRING CODE: %s\n", code)
	fmt.Fprintf(os.Stderr, "========================================\n")
	fmt.Fprintf(os.Stderr, "\nEnter this code in WhatsApp:\n")
	fmt.Fprintf(os.Stderr, "Settings -> Linked Devices -> Link a Device -> Link with phone number\n\n")

	fmt.Fprintln(os.Stderr, "Waiting for pairing confirmation...")
	for i := 0; i < 60; i++ {
		time.Sleep(1 * time.Second)
		if client.Store.ID != nil {
			fmt.Println(`{"status": "login_success", "phone": "` + client.Store.ID.User + `"}`)
			time.Sleep(3 * time.Second)
			client.Disconnect()
			return
		}
	}

	fmt.Println(`{"status": "timeout", "error": "Pairing code expired"}`)
	client.Disconnect()
	os.Exit(1)
}

func doReceive() {
	client := getClient()

	if client.Store.ID == nil {
		fmt.Println(`{"error": "not_logged_in", "message": "Run 'whatsapp-claude login' first"}`)
		os.Exit(1)
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			handleMessage(v)
		case *events.Connected:
			outputJSON(map[string]interface{}{
				"type":  "connected",
				"phone": client.Store.ID.User,
			})
		case *events.Disconnected:
			outputJSON(map[string]interface{}{
				"type": "disconnected",
			})
		}
	})

	err := client.Connect()
	if err != nil {
		fmt.Println(`{"error": "connection_failed", "message": "` + err.Error() + `"}`)
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}

func handleMessage(evt *events.Message) {
	msg := Message{
		Type:      "message",
		Sender:    evt.Info.Sender.User,
		SenderJID: evt.Info.Sender.String(),
		Chat:      evt.Info.Chat.User,
		ChatJID:   evt.Info.Chat.String(),
		Timestamp: evt.Info.Timestamp.Unix(),
		MessageID: evt.Info.ID,
		IsGroup:   evt.Info.IsGroup,
	}

	if evt.Info.PushName != "" {
		msg.SenderName = evt.Info.PushName
	}

	// Extract text content
	if evt.Message.GetConversation() != "" {
		msg.Message = evt.Message.GetConversation()
	} else if evt.Message.GetExtendedTextMessage() != nil {
		msg.Message = evt.Message.GetExtendedTextMessage().GetText()
		if ctxInfo := evt.Message.GetExtendedTextMessage().GetContextInfo(); ctxInfo != nil {
			if ctxInfo.QuotedMessage != nil {
				msg.Quote = &QuoteInfo{
					MessageID: ctxInfo.GetStanzaID(),
					Sender:    ctxInfo.GetParticipant(),
					Text:      ctxInfo.QuotedMessage.GetConversation(),
				}
			}
		}
	}

	// Extract attachment metadata
	if img := evt.Message.GetImageMessage(); img != nil {
		msg.Message = img.GetCaption()
		msg.Attachments = append(msg.Attachments, AttachmentInfo{
			Type: "image", MimeType: img.GetMimetype(), Size: uint64(img.GetFileLength()),
		})
	}
	if doc := evt.Message.GetDocumentMessage(); doc != nil {
		msg.Message = doc.GetCaption()
		msg.Attachments = append(msg.Attachments, AttachmentInfo{
			Type: "document", MimeType: doc.GetMimetype(),
			Filename: doc.GetFileName(), Size: uint64(doc.GetFileLength()),
		})
	}
	if audio := evt.Message.GetAudioMessage(); audio != nil {
		msg.Attachments = append(msg.Attachments, AttachmentInfo{
			Type: "audio", MimeType: audio.GetMimetype(), Size: uint64(audio.GetFileLength()),
		})
	}
	if video := evt.Message.GetVideoMessage(); video != nil {
		msg.Message = video.GetCaption()
		msg.Attachments = append(msg.Attachments, AttachmentInfo{
			Type: "video", MimeType: video.GetMimetype(), Size: uint64(video.GetFileLength()),
		})
	}

	outputJSON(msg)
}

func doSend(recipient, message string) {
	client := getClient()

	if client.Store.ID == nil {
		fmt.Println(`{"error": "not_logged_in", "message": "Run 'whatsapp-claude login' first"}`)
		os.Exit(1)
	}

	err := client.Connect()
	if err != nil {
		fmt.Println(`{"error": "connection_failed", "message": "` + err.Error() + `"}`)
		os.Exit(1)
	}
	defer client.Disconnect()

	// Brief stabilization sleep
	time.Sleep(1 * time.Second)

	jid := normalizeJID(recipient)
	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		Conversation: proto.String(message),
	})

	if err != nil {
		outputJSON(map[string]interface{}{
			"error":   "send_failed",
			"message": err.Error(),
		})
		os.Exit(1)
	}

	outputJSON(map[string]interface{}{
		"status":     "sent",
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
		"recipient":  jid.String(),
	})
}

func doSendFile(recipient, filePath, caption string) {
	client := getClient()

	if client.Store.ID == nil {
		fmt.Println(`{"error": "not_logged_in", "message": "Run 'whatsapp-claude login' first"}`)
		os.Exit(1)
	}

	err := client.Connect()
	if err != nil {
		fmt.Println(`{"error": "connection_failed", "message": "` + err.Error() + `"}`)
		os.Exit(1)
	}
	defer client.Disconnect()

	time.Sleep(1 * time.Second)

	data, err := os.ReadFile(filePath)
	if err != nil {
		outputJSON(map[string]interface{}{"error": "file_read_failed", "message": err.Error()})
		os.Exit(1)
	}

	uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaDocument)
	if err != nil {
		outputJSON(map[string]interface{}{"error": "upload_failed", "message": err.Error()})
		os.Exit(1)
	}

	jid := normalizeJID(recipient)
	filename := filePath[strings.LastIndex(filePath, "/")+1:]

	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			FileName:      proto.String(filename),
			Caption:       proto.String(caption),
			Mimetype:      proto.String("application/octet-stream"),
		},
	})

	if err != nil {
		outputJSON(map[string]interface{}{"error": "send_failed", "message": err.Error()})
		os.Exit(1)
	}

	outputJSON(map[string]interface{}{
		"status":     "sent",
		"message_id": resp.ID,
		"timestamp":  resp.Timestamp.Unix(),
		"recipient":  jid.String(),
		"filename":   filename,
	})
}

func doLogout() {
	client := getClient()

	if client.Store.ID == nil {
		fmt.Println(`{"status": "not_logged_in"}`)
		return
	}

	err := client.Connect()
	if err == nil {
		client.Logout(context.Background())
	}

	dbPath := filepath.Join(dataDir, "whatsapp.db")
	os.Remove(dbPath)

	fmt.Println(`{"status": "logged_out"}`)
}

func doStatus() {
	client := getClient()

	status := map[string]interface{}{
		"logged_in": client.Store.ID != nil,
	}

	if client.Store.ID != nil {
		status["phone"] = client.Store.ID.User
		status["jid"] = client.Store.ID.String()

		err := client.Connect()
		if err != nil {
			status["connected"] = false
			status["error"] = err.Error()
		} else {
			status["connected"] = client.IsConnected()
			client.Disconnect()
		}
	}

	outputJSON(status)
}

func outputJSON(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
