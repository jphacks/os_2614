package fcm

// FCM HTTP v1 の送信リクエストのワイヤ形式（12 §4）。
// 空フィールドは omitempty / ポインタで落とす。

type sendRequest struct {
	Message outboundMessage `json:"message"`
}

type outboundMessage struct {
	Token        string            `json:"token"`
	Notification *outboundNotif    `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *outboundAndroid  `json:"android,omitempty"`
}

type outboundNotif struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type outboundAndroid struct {
	CollapseKey  string                `json:"collapse_key,omitempty"`
	Notification *outboundAndroidNotif `json:"notification,omitempty"`
}

type outboundAndroidNotif struct {
	ChannelID string `json:"channel_id"`
}

// buildMessage は内部の Message を送信形式に変換する。
// collapse_key が空なら android.collapse_key を、channel_id が空なら android.notification を落とす。
func buildMessage(msg Message) outboundMessage {
	m := outboundMessage{
		Token:        msg.Token,
		Notification: &outboundNotif{Title: msg.Title, Body: msg.Body},
		Data:         msg.Data,
	}
	if msg.CollapseKey != "" || msg.ChannelID != "" {
		android := &outboundAndroid{CollapseKey: msg.CollapseKey}
		if msg.ChannelID != "" {
			android.Notification = &outboundAndroidNotif{ChannelID: msg.ChannelID}
		}
		m.Android = android
	}
	return m
}
