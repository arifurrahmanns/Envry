//! Follows Envry's live events to count new mail and show notifications.

use std::collections::VecDeque;
use std::sync::atomic::{AtomicI32, AtomicU32, AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::Duration;

use futures_util::StreamExt;
use serde::Deserialize;
use tauri::{AppHandle, Manager};
use tauri_plugin_notification::NotificationExt;
use tokio::sync::mpsc;
use tokio::time::{sleep, timeout, Instant};
use tokio_tungstenite::tungstenite::Message;

use crate::server::{Phase, Server};

/// A burst of mail (a test suite, say) becomes one notification.
const BURST_WINDOW: Duration = Duration::from_millis(400);
const BURST_MAX: Duration = Duration::from_secs(2);
const SHOW_SINGLY: usize = 2;

#[derive(Debug, Clone, Deserialize)]
pub struct Address {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub address: String,
}

impl Address {
    fn label(&self) -> &str {
        if self.name.is_empty() {
            &self.address
        } else {
            &self.name
        }
    }
}

/// The parts of a message summary (store.MessageSummary) shown here.
#[derive(Debug, Clone, Deserialize)]
pub struct Summary {
    pub id: String,
    pub from: Option<Address>,
    /// null when the message has no To header.
    pub to: Option<Vec<Address>>,
    #[serde(default)]
    pub subject: String,
    #[serde(default)]
    pub source: String,
}

#[derive(Deserialize)]
struct Event {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    data: serde_json::Value,
}

pub struct Mail {
    /// Messages received since the window was last looked at.
    pub new_count: AtomicU32,
    watching: AtomicU64,
    next_id: AtomicI32,
    /// Notification id → message id, for opening the message on click.
    targets: Mutex<VecDeque<(i32, Option<String>)>>,
    queue: mpsc::UnboundedSender<Summary>,
}

impl Mail {
    /// Creates the state and the receiving end of the notification queue.
    pub fn new() -> (Self, mpsc::UnboundedReceiver<Summary>) {
        let (tx, rx) = mpsc::unbounded_channel();
        let m = Mail {
            new_count: AtomicU32::new(0),
            watching: AtomicU64::new(0),
            next_id: AtomicI32::new(1),
            targets: Mutex::new(VecDeque::new()),
            queue: tx,
        };
        (m, rx)
    }

    /// The message a notification points at, if it was about one message.
    pub fn target(&self, notification_id: i32) -> Option<Option<String>> {
        let t = self.targets.lock().unwrap_or_else(|e| e.into_inner());
        t.iter()
            .find(|(id, _)| *id == notification_id)
            .map(|(_, m)| m.clone())
    }

    fn remember(&self, msg: Option<String>) -> i32 {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        let mut t = self.targets.lock().unwrap_or_else(|e| e.into_inner());
        if t.len() == 50 {
            t.pop_front();
        }
        t.push_back((id, msg));
        id
    }
}

/// Connects to the event stream of the server run `gen` on `http`, and
/// keeps reconnecting until that run ends.
pub fn watch(app: &AppHandle, gen: u64, http: u16) {
    let mail = app.state::<Mail>();
    if mail.watching.swap(gen, Ordering::SeqCst) == gen {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let url = format!("ws://127.0.0.1:{http}/ws");
        let mut backoff = Duration::from_millis(250);
        while is_current(&app, gen) {
            if let Ok((mut ws, _)) = tokio_tungstenite::connect_async(url.as_str()).await {
                backoff = Duration::from_millis(250);
                while let Some(Ok(msg)) = ws.next().await {
                    if !is_current(&app, gen) {
                        return;
                    }
                    if let Message::Text(text) = msg {
                        handle(&app, text.as_str());
                    }
                }
            }
            sleep(backoff).await;
            backoff = (backoff * 2).min(Duration::from_secs(5));
        }
    });
}

fn is_current(app: &AppHandle, gen: u64) -> bool {
    let (g, phase) = app.state::<Server>().current();
    g == gen && matches!(phase, Phase::Running { .. })
}

fn handle(app: &AppHandle, text: &str) {
    let Ok(ev) = serde_json::from_str::<Event>(text) else {
        return;
    };
    if ev.kind != "message.created" {
        return;
    }
    let Ok(sum) = serde_json::from_value::<Summary>(ev.data) else {
        return;
    };
    // IMAP APPEND (a mail client saving its own copy) is not new mail.
    if sum.source == "imap_append" {
        return;
    }
    if crate::window::is_looking(app) {
        return;
    }
    let mail = app.state::<Mail>();
    mail.new_count.fetch_add(1, Ordering::SeqCst);
    crate::tray::refresh(app);
    if crate::settings::notifications_enabled(app) {
        let _ = mail.queue.send(sum);
    }
}

/// Resets the new-mail count once the user looks at the window.
pub fn seen(app: &AppHandle) {
    if app.state::<Mail>().new_count.swap(0, Ordering::SeqCst) != 0 {
        crate::tray::refresh(app);
    }
}

/// Shows queued mail as notifications, folding bursts into one.
pub async fn notifier(app: AppHandle, mut rx: mpsc::UnboundedReceiver<Summary>) {
    while let Some(first) = rx.recv().await {
        let mut batch = vec![first];
        let deadline = Instant::now() + BURST_MAX;
        loop {
            let left = deadline
                .saturating_duration_since(Instant::now())
                .min(BURST_WINDOW);
            match timeout(left, rx.recv()).await {
                Ok(Some(s)) => batch.push(s),
                Ok(None) => return,
                Err(_) => break,
            }
        }
        if batch.len() <= SHOW_SINGLY {
            for s in &batch {
                notify(&app, &title_for(s), &body_for(s), Some(s.id.clone()));
            }
        } else {
            let last = batch.last().unwrap();
            let title = format!("{} new messages", batch.len());
            notify(&app, &title, &format!("Latest: {}", subject(last)), None);
        }
    }
}

fn subject(s: &Summary) -> &str {
    if s.subject.trim().is_empty() {
        "(no subject)"
    } else {
        &s.subject
    }
}

fn title_for(s: &Summary) -> String {
    match &s.from {
        Some(f) if !f.label().is_empty() => f.label().to_string(),
        _ => "New message".to_string(),
    }
}

fn body_for(s: &Summary) -> String {
    match s.to.as_ref().and_then(|t| t.first()) {
        Some(to) if !to.address.is_empty() => format!("{}\nTo {}", subject(s), to.address),
        _ => subject(s).to_string(),
    }
}

pub fn notify(app: &AppHandle, title: &str, body: &str, message: Option<String>) {
    let id = app.state::<Mail>().remember(message);
    let _ = app
        .notification()
        .builder()
        .id(id)
        .title(title)
        .body(body)
        .show();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_message_created() {
        let ev: Event = serde_json::from_str(
            r#"{"type":"message.created","data":{"id":"01J","from":{"name":"","address":"app@example.test"},
            "to":[{"name":"Ada","address":"ada@example.test"}],"cc":null,"subject":"","source":"smtp"}}"#,
        )
        .unwrap();
        let s: Summary = serde_json::from_value(ev.data).unwrap();
        assert_eq!(title_for(&s), "app@example.test");
        assert_eq!(body_for(&s), "(no subject)\nTo ada@example.test");
    }
}
