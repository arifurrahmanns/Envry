//! Commands for the bundled status page (src/main.js).

use serde::Serialize;
use tauri::{AppHandle, Manager};
use tauri_plugin_opener::OpenerExt;

use crate::server::{Phase, Server};
use crate::settings::{self, Ports};

/// What the status page shows.
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StatusView {
    #[serde(flatten)]
    phase: Phase,
    ports: Ports,
    log_path: Option<String>,
}

pub fn status_view(app: &AppHandle) -> StatusView {
    let server = app.state::<Server>();
    StatusView {
        phase: server.phase(),
        ports: settings::get(app).ports,
        log_path: server.log_path().map(|p| p.display().to_string()),
    }
}

#[tauri::command]
pub fn get_status(app: AppHandle) -> StatusView {
    status_view(&app)
}

#[tauri::command]
pub fn start_server(app: AppHandle) {
    app.state::<Server>().start(&app, settings::get(&app).ports);
}

/// Saves new ports and restarts Envry on them.
#[tauri::command]
pub fn save_ports(app: AppHandle, ports: Ports) -> Result<(), String> {
    ports.validate()?;
    settings::update(&app, |s| s.ports = ports)?;
    app.state::<Server>().restart(&app, ports);
    Ok(())
}

/// Shows the server log in the file manager.
#[tauri::command]
pub fn open_log(app: AppHandle) -> Result<(), String> {
    let path = app.state::<Server>().log_path().ok_or("no log file")?;
    app.opener()
        .reveal_item_in_dir(path)
        .map_err(|e| e.to_string())
}
