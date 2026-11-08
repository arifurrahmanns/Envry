//! Desktop settings, stored as JSON in the app's config directory.

use std::fs;
use std::path::PathBuf;
use std::sync::{Mutex, MutexGuard};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};

/// The ports Envry listens on. The server binds every interface, like the
/// CLI does, so containers and other machines can reach it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Ports {
    pub smtp: u16,
    pub pop3: u16,
    pub imap: u16,
    pub http: u16,
}

impl Default for Ports {
    fn default() -> Self {
        Ports {
            smtp: 1025,
            pop3: 1110,
            imap: 1143,
            http: 8025,
        }
    }
}

impl Ports {
    /// Each service with its port, in the order Envry opens them.
    pub fn services(&self) -> [(&'static str, u16); 4] {
        [
            ("smtp", self.smtp),
            ("pop3", self.pop3),
            ("imap", self.imap),
            ("http", self.http),
        ]
    }

    /// Rejects port 0 and two services sharing a port.
    pub fn validate(&self) -> Result<(), String> {
        let list = self.services();
        for (i, (name, port)) in list.iter().enumerate() {
            if *port == 0 {
                return Err(format!(
                    "{} port must be between 1 and 65535",
                    name.to_uppercase()
                ));
            }
            if let Some((other, _)) = list[..i].iter().find(|(_, p)| p == port) {
                return Err(format!(
                    "{} and {} cannot share port {port}",
                    other.to_uppercase(),
                    name.to_uppercase()
                ));
            }
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Settings {
    pub ports: Ports,
    pub notifications: bool,
    /// Whether the "still running in the tray" hint was shown once.
    pub tray_hint_shown: bool,
}

impl Default for Settings {
    fn default() -> Self {
        Settings {
            ports: Ports::default(),
            notifications: true,
            tray_hint_shown: false,
        }
    }
}

fn path(app: &AppHandle) -> Option<PathBuf> {
    app.path()
        .app_config_dir()
        .ok()
        .map(|d| d.join("settings.json"))
}

pub fn load(app: &AppHandle) -> Settings {
    path(app)
        .and_then(|p| fs::read(p).ok())
        .and_then(|b| serde_json::from_slice(&b).ok())
        .unwrap_or_default()
}

pub fn save(app: &AppHandle, s: &Settings) -> Result<(), String> {
    let p = path(app).ok_or("no config directory")?;
    if let Some(dir) = p.parent() {
        fs::create_dir_all(dir).map_err(|e| format!("create {}: {e}", dir.display()))?;
    }
    let json = serde_json::to_vec_pretty(s).map_err(|e| e.to_string())?;
    fs::write(&p, json).map_err(|e| format!("write {}: {e}", p.display()))
}

/// The settings in use, kept in app state; changes are saved right away.
pub type State = Mutex<Settings>;

fn lock(app: &AppHandle) -> MutexGuard<'_, Settings> {
    app.state::<State>()
        .inner()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
}

pub fn get(app: &AppHandle) -> Settings {
    lock(app).clone()
}

/// Changes the settings and saves them.
pub fn update<T>(app: &AppHandle, f: impl FnOnce(&mut Settings) -> T) -> Result<T, String> {
    let mut s = lock(app);
    let out = f(&mut s);
    save(app, &s)?;
    Ok(out)
}

pub fn notifications_enabled(app: &AppHandle) -> bool {
    lock(app).notifications
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_ports() {
        assert!(Ports::default().validate().is_ok());
        let shared = Ports {
            imap: 1025,
            ..Ports::default()
        };
        assert_eq!(
            shared.validate().unwrap_err(),
            "SMTP and IMAP cannot share port 1025"
        );
        let zero = Ports {
            http: 0,
            ..Ports::default()
        };
        assert!(zero.validate().is_err());
    }

    #[test]
    fn missing_fields_take_defaults() {
        let s: Settings = serde_json::from_str(r#"{"ports":{"smtp":2525}}"#).unwrap();
        assert_eq!(
            s.ports,
            Ports {
                smtp: 2525,
                ..Ports::default()
            }
        );
        assert!(s.notifications);
    }
}
