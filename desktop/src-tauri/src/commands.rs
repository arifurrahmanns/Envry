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
    /// Whether secure connections (TLS) are on.
    tls: bool,
    log_path: Option<String>,
}

pub fn status_view(app: &AppHandle) -> StatusView {
    let server = app.state::<Server>();
    let settings = settings::get(app);
    StatusView {
        phase: server.phase(),
        ports: settings.ports,
        tls: settings.tls,
        log_path: server.log_path().map(|p| p.display().to_string()),
    }
}

#[tauri::command]
pub fn get_status(app: AppHandle) -> StatusView {
    status_view(&app)
}

#[tauri::command]
pub fn start_server(app: AppHandle) {
    app.state::<Server>().start(&app);
}

/// Saves new ports and restarts Envry on them.
#[tauri::command]
pub fn save_ports(app: AppHandle, ports: Ports) -> Result<(), String> {
    ports.validate(settings::get(&app).tls)?;
    settings::update(&app, |s| s.ports = ports)?;
    app.state::<Server>().restart(&app);
    Ok(())
}

/// Turns secure connections (TLS) on or off and restarts Envry with them.
/// The web UI's Connection page calls this, as does the tray menu.
#[tauri::command]
pub fn set_tls(app: AppHandle, enabled: bool) -> Result<(), String> {
    if settings::get(&app).tls == enabled {
        return Ok(());
    }
    settings::update(&app, |s| s.tls = enabled)?;
    crate::tray::refresh(&app);
    app.state::<Server>().restart(&app);
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
