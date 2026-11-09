//! The tray (menu-bar) icon: server status, new-mail count and controls.

use std::sync::atomic::Ordering;

use tauri::image::Image;
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager, Wry};
use tauri_plugin_autostart::ManagerExt;

use crate::mail::Mail;
use crate::server::{Phase, Server};
use crate::update::{self, Status, Updates};
use crate::{settings, window};

const TRAY_ID: &str = "main";

/// Menu items whose text or state follows the server.
pub struct TrayMenu {
    status: MenuItem<Wry>,
    smtp: MenuItem<Wry>,
    toggle: MenuItem<Wry>,
    restart: MenuItem<Wry>,
    tls: CheckMenuItem<Wry>,
    notifications: CheckMenuItem<Wry>,
    autostart: CheckMenuItem<Wry>,
    update: MenuItem<Wry>,
}

#[derive(Clone, Copy)]
enum Look {
    Live,
    New,
    Offline,
}

fn icon(look: Look) -> Image<'static> {
    let bytes: &'static [u8] = if cfg!(target_os = "macos") {
        // Menu-bar template images: macOS tints them for light and dark bars.
        match look {
            Look::Offline => include_bytes!("../icons/tray/template-offline.png"),
            _ => include_bytes!("../icons/tray/template.png"),
        }
    } else {
        match look {
            Look::Live => include_bytes!("../icons/tray/live.png"),
            Look::New => include_bytes!("../icons/tray/new.png"),
            Look::Offline => include_bytes!("../icons/tray/offline.png"),
        }
    };
    Image::from_bytes(bytes).expect("bundled tray icon")
}

pub fn build(app: &AppHandle) -> tauri::Result<()> {
    let notify_on = settings::notifications_enabled(app);
    let autostart_on = app.autolaunch().is_enabled().unwrap_or(false);

    let status = MenuItem::with_id(app, "status", "Starting…", false, None::<&str>)?;
    let smtp = MenuItem::with_id(app, "smtp", "SMTP", false, None::<&str>)?;
    let open = MenuItem::with_id(app, "open", "Open Envry", true, None::<&str>)?;
    let toggle = MenuItem::with_id(app, "toggle", "Stop server", true, None::<&str>)?;
    let restart = MenuItem::with_id(app, "restart", "Restart server", true, None::<&str>)?;
    let tls = CheckMenuItem::with_id(
        app,
        "tls",
        "Secure connections (TLS)",
        true,
        settings::get(app).tls,
        None::<&str>,
    )?;
    let notifications = CheckMenuItem::with_id(
        app,
        "notifications",
        "Notifications",
        true,
        notify_on,
        None::<&str>,
    )?;
    let autostart = CheckMenuItem::with_id(
        app,
        "autostart",
        "Start at login",
        true,
        autostart_on,
        None::<&str>,
    )?;
    let update = MenuItem::with_id(app, "update", "Check for updates…", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit Envry", true, None::<&str>)?;
    let menu = Menu::with_items(
        app,
        &[
            &status,
            &smtp,
            &PredefinedMenuItem::separator(app)?,
            &open,
            &PredefinedMenuItem::separator(app)?,
            &toggle,
            &restart,
            &tls,
            &PredefinedMenuItem::separator(app)?,
            &notifications,
            &autostart,
            &PredefinedMenuItem::separator(app)?,
            &update,
            &quit,
        ],
    )?;

    TrayIconBuilder::with_id(TRAY_ID)
        .icon(icon(Look::Offline))
        .icon_as_template(cfg!(target_os = "macos"))
        .tooltip("Envry")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, ev| on_menu(app, ev.id().as_ref()))
        .on_tray_icon_event(|tray, ev| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = ev
            {
                window::show(tray.app_handle());
            }
        })
        .build(app)?;

    app.manage(TrayMenu {
        status,
        smtp,
        toggle,
        restart,
        tls,
        notifications,
        autostart,
        update,
    });
    refresh(app);
    Ok(())
}

fn on_menu(app: &AppHandle, id: &str) {
    let server = app.state::<Server>();
    match id {
        "open" => window::show(app),
        "toggle" => match server.phase() {
            Phase::Running { .. } | Phase::Starting => server.stop(app),
            _ => server.start(app),
        },
        "restart" => server.restart(app),
        "tls" => {
            let on = !settings::get(app).tls;
            let _ = crate::commands::set_tls(app.clone(), on);
        }
        "notifications" => {
            let on = settings::update(app, |s| {
                s.notifications = !s.notifications;
                s.notifications
            })
            .unwrap_or_else(|_| settings::notifications_enabled(app));
            let _ = app.state::<TrayMenu>().notifications.set_checked(on);
        }
        "autostart" => {
            let al = app.autolaunch();
            let _ = if al.is_enabled().unwrap_or(false) {
                al.disable()
            } else {
                al.enable()
            };
            let _ = app
                .state::<TrayMenu>()
                .autostart
                .set_checked(al.is_enabled().unwrap_or(false));
        }
        "update" => match app.state::<Updates>().status() {
            Status::Ready(_) => update::install(app),
            Status::Idle => update::check_now(app),
            _ => {}
        },
        "quit" => app.exit(0),
        _ => {}
    }
}

/// Brings the icon, tooltip, menu and window badge up to date.
pub fn refresh(app: &AppHandle) {
    let phase = app.state::<Server>().phase();
    let new = app.state::<Mail>().new_count.load(Ordering::SeqCst);
    let running = matches!(phase, Phase::Running { .. });

    let (status, look) = match &phase {
        Phase::Running { .. } if new > 0 => (format!("● Live — {new} new"), Look::New),
        Phase::Running { .. } => ("● Live".to_string(), Look::Live),
        Phase::Starting => ("Starting…".to_string(), Look::Offline),
        Phase::Conflict { .. } => ("Offline — port in use".to_string(), Look::Offline),
        Phase::Failed { .. } => ("Offline — stopped with an error".to_string(), Look::Offline),
        Phase::Stopped => ("Offline".to_string(), Look::Offline),
    };
    let update = app.state::<Updates>().status();
    let mut tooltip = match &phase {
        Phase::Running { .. } if new > 0 => format!("Envry — Live · {new} new"),
        Phase::Running { .. } => "Envry — Live".to_string(),
        _ => format!("Envry — {}", status.trim_start_matches("● ")),
    };
    if let Status::Ready(version) = &update {
        tooltip.push_str(&format!(" · Update {version} ready"));
    }

    if let Some(tray) = app.tray_by_id(TRAY_ID) {
        let _ = tray.set_icon_with_as_template(Some(icon(look)), cfg!(target_os = "macos"));
        let _ = tray.set_tooltip(Some(&tooltip));
        #[cfg(target_os = "macos")]
        let _ = tray.set_title(if new > 0 { Some(new.to_string()) } else { None });
    }
    if let Some(m) = app.try_state::<TrayMenu>() {
        let _ = m.status.set_text(&status);
        let smtp = match &phase {
            Phase::Running { ports, .. } => format!("SMTP 127.0.0.1:{}", ports.smtp),
            _ => format!("SMTP 127.0.0.1:{} (offline)", settings::get(app).ports.smtp),
        };
        let _ = m.smtp.set_text(smtp);
        let busy = running || matches!(phase, Phase::Starting);
        let _ = m
            .toggle
            .set_text(if busy { "Stop server" } else { "Start server" });
        let _ = m.restart.set_enabled(running);
        let _ = m.tls.set_checked(settings::get(app).tls);
        let (text, enabled) = match &update {
            Status::Idle => ("Check for updates…".to_string(), true),
            Status::Checking => ("Checking for updates…".to_string(), false),
            Status::Downloading(v) => (format!("Downloading Envry {v}…"), false),
            Status::Ready(v) => (format!("Restart to update to {v}"), true),
            Status::Installing(v) => (format!("Installing Envry {v}…"), false),
        };
        let _ = m.update.set_text(text);
        let _ = m.update.set_enabled(enabled);
    }
    window::set_badge(app, new);
}
