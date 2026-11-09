//! Envry desktop app: runs the bundled Envry server, shows its web UI and
//! keeps a tray icon with its status and new mail.

mod certificate;
mod commands;
mod mail;
mod server;
mod settings;
mod tray;
mod update;
mod window;

use std::fs;
use std::path::Path;
use std::sync::Mutex;

use tauri::{AppHandle, Emitter, Manager, RunEvent, WindowEvent};
use tauri_plugin_autostart::MacosLauncher;
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_window_state::StateFlags;

use mail::Mail;
use server::{Phase, Server};

/// The log is cut back to empty when it grows past this at startup.
const LOG_LIMIT: u64 = 2 * 1024 * 1024;

/// Called whenever the server changes phase: updates the tray, the window
/// and the status page, and follows the new run's mail.
pub(crate) fn phase_changed(app: &AppHandle) {
    let (gen, phase) = app.state::<Server>().current();
    tray::refresh(app);
    window::follow(app, &phase);
    let _ = app.emit("status", commands::status_view(app));
    if let Phase::Running { ports, .. } = &phase {
        mail::watch(app, gen, ports.http);
    }
}

fn trim_log(path: &Path) {
    if fs::metadata(path).is_ok_and(|m| m.len() > LOG_LIMIT) {
        let _ = fs::write(path, b"");
    }
}

/// Explains, once, that closing the window keeps Envry running.
fn tray_hint(app: &AppHandle) {
    let first = settings::update(app, |s| !std::mem::replace(&mut s.tray_hint_shown, true))
        .unwrap_or(false);
    if first {
        let place = if cfg!(target_os = "macos") {
            "menu bar"
        } else {
            "system tray"
        };
        let _ = app
            .notification()
            .builder()
            .title("Envry is still running")
            .body(format!(
                "It keeps catching mail in the {place}. Choose Quit Envry there to stop it."
            ))
            .show();
    }
}

pub fn run() {
    let (mail, queue) = Mail::new();
    tauri::Builder::default()
        // Must come first: a second launch only brings this window forward.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            window::show(app)
        }))
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_autostart::init(
            MacosLauncher::LaunchAgent,
            Some(vec!["--hidden"]),
        ))
        .plugin(
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(
                    StateFlags::all() & !StateFlags::VISIBLE & !StateFlags::DECORATIONS,
                )
                .build(),
        )
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(Server::default())
        .manage(mail)
        .manage(update::Updates::default())
        .manage(window::LastPage::default())
        .invoke_handler(tauri::generate_handler![
            commands::get_status,
            commands::start_server,
            commands::save_ports,
            commands::open_log,
            commands::set_tls,
            certificate::certificate_status,
            certificate::trust_certificate,
        ])
        .setup(move |app| {
            let handle = app.handle().clone();
            app.manage::<settings::State>(Mutex::new(settings::load(&handle)));

            if let Ok(dir) = app.path().app_log_dir() {
                let log = dir.join("envry.log");
                trim_log(&log);
                app.state::<Server>().set_log_path(log);
            }

            // Started at login: stay in the tray.
            let hidden = std::env::args().any(|a| a == "--hidden");
            window::create(&handle, !hidden)?;
            tray::build(&handle)?;

            let on_tap = handle.clone();
            handle.notification().on_action(move |a| {
                if a.action_id() != "tap" {
                    return;
                }
                let id = a.notification().map(|n| n.id());
                if id == Some(update::NOTIFICATION_ID) {
                    update::install(&on_tap);
                    return;
                }
                let target = id.and_then(|id| on_tap.state::<Mail>().target(id));
                window::open_message(&on_tap, target.flatten());
            })?;
            tauri::async_runtime::spawn(mail::notifier(handle.clone(), queue));
            update::start(&handle);

            app.state::<Server>().start(&handle);
            Ok(())
        })
        .on_window_event(|w, ev| match ev {
            WindowEvent::CloseRequested { api, .. } => {
                api.prevent_close();
                let _ = w.hide();
                tray_hint(w.app_handle());
            }
            WindowEvent::Focused(true) => mail::seen(w.app_handle()),
            _ => {}
        })
        .build(tauri::generate_context!())
        .expect("error while building the Envry app")
        .run(|app, ev| match ev {
            // The window only hides; keep running until Quit.
            RunEvent::ExitRequested {
                api, code: None, ..
            } => api.prevent_exit(),
            RunEvent::Exit => app.state::<Server>().stop_and_wait(),
            #[cfg(target_os = "macos")]
            RunEvent::Reopen { .. } => window::show(app),
            _ => {}
        });
}
