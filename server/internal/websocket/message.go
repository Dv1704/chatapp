package websocket

type Message struct {
	Type    string      `json:"type"`    // "chat", "user_list", etc.
	From    string      `json:"from,omitempty"`
	To      string      `json:"to,omitempty"`
	Content interface{} `json:"content"`
}