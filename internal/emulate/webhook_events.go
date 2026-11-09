package emulate

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arifurrahmanns/envry/internal/store"
)

// The shapes below follow each provider's webhook documentation, so an
// app's handler (and its signature check) works the same against Envry.

const bounceText = "550 5.1.1 The email account that you tried to reach does not exist."

func buildRequests(st store.ProviderWebhook, msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	switch msg.provider {
	case Resend:
		return resendEvents(st.Secret, msg, event, rcpts, now)
	case SendGrid:
		return sendgridEvents(st.Secret, msg, event, rcpts, now)
	case Postmark:
		return postmarkEvents(msg, event, rcpts, now)
	case Mailgun:
		return mailgunEvents(st.Secret, msg, event, rcpts, now)
	case SES:
		return sesEvents(msg, event, rcpts, now)
	}
	return nil, ErrUnknownProvider
}

func jsonRequest(rcpt string, v any, header http.Header) (request, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return request{}, err
	}
	if header == nil {
		header = http.Header{}
	}
	return request{recipient: rcpt, body: b, contentType: "application/json", header: header}, nil
}

// ---- Resend: Svix-signed JSON, one event per recipient ----

func resendEvents(secret string, msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	key, err := svixKey(secret)
	if err != nil {
		return nil, err
	}
	types := map[string]string{EventDelivered: "email.delivered", EventBounced: "email.bounced", EventComplained: "email.complained"}
	var out []request
	for _, rcpt := range rcpts {
		data := map[string]any{
			"email_id":   msg.id,
			"from":       msg.from,
			"to":         []string{rcpt},
			"subject":    msg.subject,
			"created_at": msg.at.UTC().Format("2006-01-02 15:04:05.000000+00"),
		}
		if len(msg.meta.values) > 0 {
			data["tags"] = msg.meta.values
		}
		if event == EventBounced {
			data["bounce"] = map[string]string{"message": bounceText, "subType": "General", "type": "Permanent"}
		}
		body, err := json.Marshal(map[string]any{
			"type": types[event], "created_at": now.Format("2006-01-02T15:04:05.000Z"), "data": data,
		})
		if err != nil {
			return nil, err
		}
		id := "msg_" + randomHex(12)
		ts := strconv.FormatInt(now.Unix(), 10)
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(id + "." + ts + "." + string(body)))
		h := http.Header{}
		h.Set("Svix-Id", id)
		h.Set("Svix-Timestamp", ts)
		h.Set("Svix-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		h.Set("User-Agent", "Svix-Webhooks/1.0")
		out = append(out, request{recipient: rcpt, body: body, contentType: "application/json", header: h})
	}
	return out, nil
}

// svixKey decodes a Svix signing secret: whsec_ followed by base64.
func svixKey(secret string) ([]byte, error) {
	b64, ok := strings.CutPrefix(secret, "whsec_")
	if !ok {
		return nil, errors.New("resend signing secret must start with whsec_")
	}
	return base64.StdEncoding.DecodeString(b64)
}

// ---- SendGrid: one ECDSA-signed array with every recipient's event ----

func sendgridEvents(secret string, msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	key, err := sendgridKey(secret)
	if err != nil {
		return nil, err
	}
	names := map[string]string{EventDelivered: "delivered", EventBounced: "bounce", EventComplained: "spamreport"}
	var list []map[string]any
	for i, rcpt := range rcpts {
		ev := map[string]any{}
		// Custom arguments come back as top-level fields.
		for k, v := range msg.meta.values {
			ev[k] = v
		}
		ev["email"] = rcpt
		ev["timestamp"] = now.Unix()
		ev["event"] = names[event]
		ev["sg_event_id"] = randomHex(11)
		ev["sg_message_id"] = fmt.Sprintf("%s.envry.%d", msg.id, i)
		ev["smtp-id"] = "<" + msg.id + "@envry.local>"
		if len(msg.meta.tags) > 0 {
			ev["category"] = msg.meta.tags
		}
		switch event {
		case EventDelivered:
			ev["response"] = "250 OK"
			ev["ip"] = "127.0.0.1"
			ev["tls"] = 1
		case EventBounced:
			ev["type"] = "bounce"
			ev["reason"] = bounceText
			ev["status"] = "5.1.1"
			ev["bounce_classification"] = "Invalid Address"
		}
		list = append(list, ev)
	}
	body, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	// The Signed Event Webhook: ECDSA over SHA-256 of timestamp + body.
	ts := strconv.FormatInt(now.Unix(), 10)
	digest := sha256.Sum256(append([]byte(ts), body...))
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	h.Set("X-Twilio-Email-Event-Webhook-Signature", base64.StdEncoding.EncodeToString(sig))
	h.Set("X-Twilio-Email-Event-Webhook-Timestamp", ts)
	h.Set("User-Agent", "SendGrid Event API")
	return []request{{body: body, contentType: "application/json", header: h}}, nil
}

// ---- Postmark: one JSON record per recipient, no signature ----

func postmarkEvents(msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	stream := msg.meta.stream
	if stream == "" {
		stream = "outbound"
	}
	tag := ""
	if len(msg.meta.tags) > 0 {
		tag = msg.meta.tags[0]
	}
	metadata := msg.meta.values
	if metadata == nil {
		metadata = map[string]string{}
	}
	at := now.Format(time.RFC3339)
	var out []request
	for _, rcpt := range rcpts {
		var rec map[string]any
		switch event {
		case EventDelivered:
			rec = map[string]any{
				"RecordType": "Delivery", "ServerID": 1, "MessageStream": stream, "MessageID": msg.id,
				"Recipient": rcpt, "Tag": tag, "DeliveredAt": at,
				"Details": "Delivered by Envry; nothing left this computer.", "Metadata": metadata,
			}
		case EventBounced:
			rec = map[string]any{
				"RecordType": "Bounce", "ID": randomID(), "Type": "HardBounce", "TypeCode": 1, "Name": "Hard bounce",
				"Tag": tag, "MessageID": msg.id, "ServerID": 1, "MessageStream": stream,
				"Description": "The server was unable to deliver your message (ex: unknown user, mailbox not found).",
				"Details":     "smtp;" + bounceText, "Email": rcpt, "From": msg.sender, "BouncedAt": at,
				"DumpAvailable": false, "Inactive": true, "CanActivate": true, "Subject": msg.subject, "Metadata": metadata,
			}
		case EventComplained:
			rec = map[string]any{
				"RecordType": "SpamComplaint", "ID": randomID(), "Type": "SpamComplaint", "TypeCode": 512,
				"Name": "Spam complaint", "Tag": tag, "MessageID": msg.id, "ServerID": 1, "MessageStream": stream,
				"Description": "The subscriber explicitly marked this message as spam.",
				"Details":     "Reported by Envry for complaint@ addresses.", "Email": rcpt, "From": msg.sender,
				"BouncedAt": at, "DumpAvailable": false, "Inactive": true, "CanActivate": false,
				"Subject": msg.subject, "Metadata": metadata,
			}
		}
		h := http.Header{}
		h.Set("User-Agent", "Postmark")
		rq, err := jsonRequest(rcpt, rec, h)
		if err != nil {
			return nil, err
		}
		out = append(out, rq)
	}
	return out, nil
}

// ---- Mailgun: one HMAC-signed JSON event per recipient ----

func mailgunEvents(signingKey string, msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	domain := msg.meta.domain
	if domain == "" {
		domain = domainOf(msg.sender)
	}
	tags := msg.meta.tags
	if tags == nil {
		tags = []string{}
	}
	vars := msg.meta.values
	if vars == nil {
		vars = map[string]string{}
	}
	var out []request
	for _, rcpt := range rcpts {
		data := map[string]any{
			"id":               randomHex(11),
			"timestamp":        float64(now.UnixMicro()) / 1e6,
			"recipient":        rcpt,
			"recipient-domain": domainOf(rcpt),
			"message": map[string]any{
				"headers": map[string]string{
					"to": rcpt, "message-id": msg.id + "@" + domain, "from": msg.from, "subject": msg.subject,
				},
				"attachments": []any{},
				"size":        msg.size,
			},
			"tags":           tags,
			"user-variables": vars,
			"envelope":       map[string]string{"transport": "smtp", "sender": msg.sender, "targets": rcpt},
		}
		switch event {
		case EventDelivered:
			data["event"], data["log-level"] = "delivered", "info"
			data["delivery-status"] = map[string]any{"code": 250, "message": "OK", "description": "", "attempt-no": 1, "tls": true}
		case EventBounced:
			data["event"], data["log-level"], data["severity"], data["reason"] = "failed", "error", "permanent", "bounce"
			data["delivery-status"] = map[string]any{"code": 550, "message": bounceText, "description": "", "attempt-no": 1}
		case EventComplained:
			data["event"], data["log-level"] = "complained", "warn"
		}
		token := randomHex(25)
		ts := strconv.FormatInt(now.Unix(), 10)
		mac := hmac.New(sha256.New, []byte(signingKey))
		mac.Write([]byte(ts + token))
		h := http.Header{}
		h.Set("User-Agent", "mailgun/treq-16.12.0")
		rq, err := jsonRequest(rcpt, map[string]any{
			"signature":  map[string]string{"timestamp": ts, "token": token, "signature": hex.EncodeToString(mac.Sum(nil))},
			"event-data": data,
		}, h)
		if err != nil {
			return nil, err
		}
		out = append(out, rq)
	}
	return out, nil
}

// ---- Amazon SES: SNS notifications, one per recipient ----

// SNS signs notifications with an AWS certificate Envry does not have, so
// apps that verify SNS signatures must skip that check in development.
const sesTopic = "arn:aws:sns:us-east-1:000000000000:envry-ses-events"

func sesEvents(msg sentMessage, event string, rcpts []string, now time.Time) ([]request, error) {
	tags := map[string][]string{}
	for k, v := range msg.meta.values {
		tags[k] = []string{v}
	}
	mail := map[string]any{
		"timestamp":        msg.at.UTC().Format(time.RFC3339Nano),
		"messageId":        msg.id,
		"source":           msg.sender,
		"sourceArn":        "arn:aws:ses:us-east-1:000000000000:identity/" + domainOf(msg.sender),
		"sendingAccountId": "000000000000",
		"destination":      msg.to,
		"headersTruncated": false,
		"commonHeaders":    map[string]any{"from": []string{msg.from}, "to": msg.to, "messageId": msg.id, "subject": msg.subject},
		"tags":             tags,
	}
	at := now.Format(time.RFC3339Nano)
	var out []request
	for _, rcpt := range rcpts {
		// Both field names: SES notifications say notificationType, SES
		// event publishing says eventType.
		ev := map[string]any{"mail": mail}
		switch event {
		case EventDelivered:
			ev["notificationType"], ev["eventType"] = "Delivery", "Delivery"
			ev["delivery"] = map[string]any{
				"timestamp": at, "processingTimeMillis": 1000, "recipients": []string{rcpt},
				"smtpResponse": "250 2.0.0 OK", "reportingMTA": "envry.local", "remoteMtaIp": "127.0.0.1",
			}
		case EventBounced:
			ev["notificationType"], ev["eventType"] = "Bounce", "Bounce"
			ev["bounce"] = map[string]any{
				"bounceType": "Permanent", "bounceSubType": "General", "timestamp": at,
				"feedbackId": randomHex(16), "reportingMTA": "dsn; envry.local",
				"bouncedRecipients": []map[string]string{{
					"emailAddress": rcpt, "action": "failed", "status": "5.1.1", "diagnosticCode": "smtp; " + bounceText,
				}},
			}
		case EventComplained:
			ev["notificationType"], ev["eventType"] = "Complaint", "Complaint"
			ev["complaint"] = map[string]any{
				"timestamp": at, "feedbackId": randomHex(16), "complaintFeedbackType": "abuse", "userAgent": "Envry",
				"complainedRecipients": []map[string]string{{"emailAddress": rcpt}},
			}
		}
		inner, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		id := uuid()
		body, err := json.Marshal(map[string]string{
			"Type": "Notification", "MessageId": id, "TopicArn": sesTopic, "Message": string(inner), "Timestamp": at,
			"SignatureVersion": "1", "Signature": "ENVRY-UNSIGNED",
			"SigningCertURL": "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-envry.pem",
			"UnsubscribeURL": "http://127.0.0.1/envry/unsubscribe",
		})
		if err != nil {
			return nil, err
		}
		h := http.Header{}
		h.Set("X-Amz-Sns-Message-Type", "Notification")
		h.Set("X-Amz-Sns-Message-Id", id)
		h.Set("X-Amz-Sns-Topic-Arn", sesTopic)
		h.Set("User-Agent", "Amazon Simple Notification Service Agent")
		out = append(out, request{recipient: rcpt, body: body, contentType: "text/plain; charset=UTF-8", header: h})
	}
	return out, nil
}

func domainOf(address string) string {
	if i := strings.LastIndexByte(address, '@'); i >= 0 {
		return strings.TrimSuffix(address[i+1:], ">")
	}
	return address
}

func randomID() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<53))
	return n.Int64()
}

func uuid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
