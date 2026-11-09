//! Updates from GitHub Releases.
//!
//! The app reads the update manifest named in tauri.conf.json
//! (plugins.updater) shortly after it starts and then every few hours,
//! downloads a newer version in the background, and installs it when the
//! user chooses "Restart to update" in the tray or clicks the notification.
//! Every download is checked against the public key in the same config
//! before anything is installed.

use std::sync::{Mutex, MutexGuard};
use std::thread;
use std::time::Duration;

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_updater::{Update, UpdaterExt};

use crate::server::Server;

const FIRST_CHECK: Duration = Duration::from_secs(30);
const CHECK_EVERY: Duration = Duration::from_secs(6 * 60 * 60);

/// The id of the "update ready" notification; clicking it installs.
pub const NOTIFICATION_ID: i32 = i32::MAX;

/// The updater's state, also sent to the web UI as the "update-status"
/// event: {"state":"ready","version":"0.2.3"}, or {"state":"idle"}.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
#[serde(tag = "state", content = "version", rename_all = "camelCase")]
pub enum Status {
    #[default]
    Idle,
    Checking,
    Downloading(String),
    Ready(String),
    Installing(String),
}

#[derive(Default)]
pub struct Updates {
    status: Mutex<Status>,
    /// The downloaded update and its package, kept until installed.
    ready: Mutex<Option<(Update, Vec<u8>)>>,
}

fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(|e| e.into_inner())
}

impl Updates {
    pub fn status(&self) -> Status {
        lock(&self.status).clone()
    }

    fn set(&self, app: &AppHandle, status: Status) {
        *lock(&self.status) = status.clone();
        crate::tray::refresh(app);
        // The web UI's update notice (web/src/hooks/use-app-update.ts).
        let _ = app.emit("update-status", status);
    }
}

/// The updater's state, for the web UI's update notice.
#[tauri::command]
pub fn update_status(app: AppHandle) -> Status {
    app.state::<Updates>().status()
}

/// "Restart to update" in the web UI.
#[tauri::command]
pub fn install_update(app: AppHandle) {
    install(&app);
}

/// Starts the background checks. Builds made with `tauri dev` (debug) only
/// check when asked, so a development copy never replaces itself.
pub fn start(app: &AppHandle) {
    if cfg!(debug_assertions) {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(FIRST_CHECK).await;
        loop {
            check(&app, false).await;
            tokio::time::sleep(CHECK_EVERY).await;
        }
    });
}

/// "Check for updates…" in the tray: like a background check, but it also
/// says when Envry is up to date or the check failed.
pub fn check_now(app: &AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move { check(&app, true).await });
}

async fn check(app: &AppHandle, asked: bool) {
    let updates = app.state::<Updates>();
    match updates.status() {
        Status::Idle => {}
        Status::Ready(version) => {
            if asked {
                notify_ready(app, &version);
            }
            return;
        }
        _ => return,
    }

    updates.set(app, Status::Checking);
    let found = match app.updater() {
        Ok(updater) => updater.check().await,
        Err(e) => Err(e),
    };
    match found {
        Ok(Some(update)) => {
            let version = update.version.clone();
            updates.set(app, Status::Downloading(version.clone()));
            match update.download(|_, _| {}, || {}).await {
                Ok(bytes) => {
                    *lock(&updates.ready) = Some((update, bytes));
                    updates.set(app, Status::Ready(version.clone()));
                    notify_ready(app, &version);
                }
                Err(e) => {
                    updates.set(app, Status::Idle);
                    if asked {
                        notify(app, "Couldn't download the update", &e.to_string());
                    }
                }
            }
        }
        Ok(None) => {
            updates.set(app, Status::Idle);
            if asked {
                let current = app.package_info().version.to_string();
                notify(
                    app,
                    "Envry is up to date",
                    &format!("You have the latest version, {current}."),
                );
            }
        }
        Err(e) => {
            updates.set(app, Status::Idle);
            if asked {
                notify(app, "Couldn't check for updates", &e.to_string());
            }
        }
    }
}

/// Stops the server, installs the downloaded update and starts the new
/// version. On Windows the installer takes over: the app exits and the
/// installer reopens Envry when it is done.
pub fn install(app: &AppHandle) {
    let updates = app.state::<Updates>();
    let Some((update, bytes)) = lock(&updates.ready).take() else {
        return;
    };
    let version = update.version.clone();
    updates.set(app, Status::Installing(version.clone()));

    let app = app.clone();
    thread::spawn(move || {
        // The installer replaces envry.exe, which must not be running.
        app.state::<Server>().stop_and_wait();
        match update.install(&bytes) {
            Ok(()) => app.restart(),
            Err(e) => {
                let updates = app.state::<Updates>();
                *lock(&updates.ready) = Some((update, bytes));
                updates.set(&app, Status::Ready(version));
                notify(&app, "Couldn't install the update", &e.to_string());
                app.state::<Server>().start(&app);
            }
        }
    });
}

fn notify_ready(app: &AppHandle, version: &str) {
    let _ = app
        .notification()
        .builder()
        .id(NOTIFICATION_ID)
        .title(format!("Envry {version} is ready"))
        .body("Click to restart and update, or choose Restart to update in the tray menu.")
        .show();
}

fn notify(app: &AppHandle, title: &str, body: &str) {
    let _ = app.notification().builder().title(title).body(body).show();
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The shape web/src/hooks/use-app-update.ts reads.
    #[test]
    fn status_as_json() {
        let json = |s: Status| serde_json::to_string(&s).unwrap();
        assert_eq!(json(Status::Idle), r#"{"state":"idle"}"#);
        assert_eq!(
            json(Status::Ready("0.2.3".into())),
            r#"{"state":"ready","version":"0.2.3"}"#
        );
        assert_eq!(
            json(Status::Downloading("0.2.3".into())),
            r#"{"state":"downloading","version":"0.2.3"}"#
        );
    }
}
