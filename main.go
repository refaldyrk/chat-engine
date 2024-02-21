package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/mervick/aes-everywhere/go/aes256"
	"net/http"
	"sync"
)

type Client struct {
	ID          string
	MessageChan chan string
}

type Room struct {
	ID           string
	Clients      map[string]*Client
	AddClient    chan *Client
	RemoveClient chan string
	Secret       string
}

type ChatServer struct {
	Rooms map[string]*Room
	mu    sync.Mutex
}

func generateRoomSecret(roomID string) string {
	hash := sha256.New()
	hash.Write([]byte(roomID))
	secret := hex.EncodeToString(hash.Sum(nil))

	return secret
}

func NewChatServer() *ChatServer {
	return &ChatServer{
		Rooms: make(map[string]*Room),
	}
}

func (cs *ChatServer) GetRoom(roomID string) *Room {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if room, ok := cs.Rooms[roomID]; ok && room.Secret != "" {
		return room
	}

	// Create a new room with a unique secret
	newRoom := &Room{
		ID:           roomID,
		Clients:      make(map[string]*Client),
		AddClient:    make(chan *Client),
		RemoveClient: make(chan string),
		Secret:       generateRoomSecret(roomID),
	}
	cs.Rooms[roomID] = newRoom

	go newRoom.Run()
	return newRoom
}

func (r *Room) Run() {
	for {
		select {
		case client := <-r.AddClient:
			r.Clients[client.ID] = client
		case clientID := <-r.RemoveClient:
			delete(r.Clients, clientID)
		}
	}
}

func (r *Room) Broadcast(message string) {
	for _, client := range r.Clients {
		client.MessageChan <- message
	}
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room_id")
	if roomID == "" {
		http.Error(w, "Room ID is required", http.StatusBadRequest)
		return
	}

	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		http.Error(w, "Client ID is required", http.StatusBadRequest)
		return
	}

	secret := generateRoomSecret(roomID)

	client := &Client{
		ID:          clientID,
		MessageChan: make(chan string),
	}

	chatServer := chatServerInstance
	room := chatServer.GetRoom(roomID)

	room.AddClient <- client

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	fmt.Fprintf(w, "data: Welcome to the chat room %s!\n\n", roomID)
	w.(http.Flusher).Flush()

	for {
		select {
		case message := <-client.MessageChan:
			decryptedMessage := aes256.Decrypt(message, secret)

			fmt.Fprintf(w, "data: %s\n\n", decryptedMessage)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			room.RemoveClient <- client.ID
			return
		}
	}
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := r.FormValue("room_id")
	if roomID == "" {
		http.Error(w, "Room ID is required", http.StatusBadRequest)
		return
	}

	message := r.FormValue("message")
	if message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	chatServer := chatServerInstance
	room := chatServer.GetRoom(roomID)

	encryptedMessage := aes256.Encrypt(fmt.Sprintf("%s:%s", r.FormValue("client_id"), message), room.Secret)
	room.Broadcast(encryptedMessage)

	w.WriteHeader(http.StatusOK)
}

var chatServerInstance = NewChatServer()

func main() {
	http.HandleFunc("/", handleChat)
	http.HandleFunc("/send", handleSend)

	fmt.Println("Server listening on :8080")
	http.ListenAndServe(":8080", nil)
}
